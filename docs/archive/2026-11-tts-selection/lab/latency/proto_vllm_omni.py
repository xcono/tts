"""Same latency protocol as proto_omnivoice_server.py, request body for vLLM-Omni (OmniVoice, voice clone, pcm).

Usage: python3 proto_vllm_omni.py PORT            -> protocol (2 warm-up + 5x short/long, 15 s pauses)
       python3 proto_vllm_omni.py PORT mini          -> same, but 1 repeat instead of 5 (4 requests total)
       python3 proto_vllm_omni.py PORT once OUT.wav [short|long]  -> one request, saved as WAV
"""
import http.client, json, subprocess, sys, time, statistics, wave
PORT=int(sys.argv[1]); PAUSE=15
SHORT="Тесты прошли, изменения закоммичены, pull request открыт."
LONG="Репозиторий лежит в корневой папке, все Go-файлы целы и не изменены, ветка main, последний коммит прежний."
REF_AUDIO="file:///ref/ref_stas.wav"  # llm/qwen3-tts/ref_stas.wav mounted read-only
VOICE=__import__("os").environ.get("VOICE")  # uploaded voice name (POST /v1/audio/voices) instead of inline ref_audio
REF_TEXT=("Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. "
          "Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток.")
def gpu():
    return subprocess.run(["nvidia-smi","-i","1","--query-gpu=power.draw,temperature.gpu,utilization.gpu,memory.used","--format=csv,noheader,nounits"],capture_output=True,text=True).stdout.strip()
def req(text, keep=False):
    b={"model":"k2-fsa/OmniVoice","input":text,"response_format":"pcm","language":"ru",
       "ref_audio":REF_AUDIO,"ref_text":REF_TEXT,
       "extra_params":{"num_inference_steps":32,"guidance_scale":2.0}}
    if VOICE: b.pop("ref_audio"); b["voice"]=VOICE
    c=http.client.HTTPConnection("127.0.0.1",PORT,timeout=300)
    t=time.perf_counter(); c.request("POST","/v1/audio/speech",json.dumps(b,ensure_ascii=False).encode(),{"Content-Type":"application/json"})
    r=c.getresponse(); first=None; n=0; buf=bytearray()
    while True:
        x=r.read1(65536)
        if not x: break
        if first is None: first=time.perf_counter()-t
        n+=len(x)
        if keep: buf+=x
    dt=time.perf_counter()-t; c.close()
    if r.status!=200: print(bytes(buf[:2000]).decode(errors="replace"),flush=True)
    return first,dt,r.status,n/48000,bytes(buf)
if len(sys.argv)>2 and sys.argv[2]=="once":
    text=LONG if (len(sys.argv)<5 or sys.argv[4]=="long") else SHORT
    g=gpu(); f,dt,st,dur,pcm=req(text,keep=True)
    print("once",st,"first",round(f or -1,3),"full",round(dt,3),"audio",round(dur,2),"s | before: W,C,util%,MiB =",g,"| after MiB:",gpu(),flush=True)
    if st==200:
        with wave.open(sys.argv[3],"wb") as w: w.setnchannels(1); w.setsampwidth(2); w.setframerate(24000); w.writeframes(pcm)
    sys.exit(0 if st==200 else 1)
for name,text in (("warm",SHORT),("warm",LONG)):
    f,dt,st,dur,_=req(text); print(name,st,round(dt,3),flush=True)
    if st!=200: sys.exit(1)
res={"short":[],"long":[]}
REPEATS=1 if (len(sys.argv)>2 and sys.argv[2]=="mini") else 5  # mini: request budget left for 1 repeat only
for i in range(REPEATS):
    for name,text in (("short",SHORT),("long",LONG)):
        time.sleep(PAUSE); g=gpu(); f,dt,st,dur,_=req(text); res[name].append(dt)
        print(name,i,st,"first",round(f,3),"full",round(dt,3),"audio",round(dur,2),"s | before: W,C,util%,MiB =",g,flush=True)
for k,v in res.items(): print(k,"median",round(statistics.median(v),3),"min",round(min(v),3),"max",round(max(v),3))
