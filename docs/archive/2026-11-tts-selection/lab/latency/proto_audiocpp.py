"""Latency protocol: fixed phrases, pause between requests, power/temp logged."""
import http.client, json, subprocess, sys, time, statistics
PORT=int(sys.argv[1]); OUT=sys.argv[2]; PAUSE=15
REF=("Это нелегко. Ты думаешь, меня окружают прекрасные люди. Лучшей командой желать нельзя. "
     "Но всё равно что-то может пройти не так. Этот мир не прощает ошибок. Он жесток.")
SHORT="Тесты прошли, изменения закоммичены, pull request открыт."
LONG="Репозиторий лежит в корневой папке, все Go-файлы целы и не изменены, ветка main, последний коммит прежний."
def gpu():
    o=subprocess.run(["nvidia-smi","-i","1","--query-gpu=power.draw,temperature.gpu,utilization.gpu,memory.used","--format=csv,noheader,nounits"],capture_output=True,text=True).stdout.strip()
    return o
def req(text):
    b={"model":"omnivoice","input":text,"voice_ref":"/ref/ref_stas.wav","reference_text":REF,
       "options":{"language":"ru","num_inference_steps":32,"guidance_scale":2.0}}
    c=http.client.HTTPConnection("127.0.0.1",PORT,timeout=300)
    t=time.perf_counter(); c.request("POST","/v1/audio/speech",json.dumps(b,ensure_ascii=False).encode(),{"Content-Type":"application/json"})
    r=c.getresponse(); d=r.read(); dt=time.perf_counter()-t; c.close(); return dt,r.status,d
for name,text in (("warm",SHORT),("warm",LONG)):
    dt,st,d=req(text); print(name,st,round(dt,3),flush=True)
    if st!=200: print(d[:1500].decode(errors="replace")); sys.exit(1)
res={"short":[],"long":[]}
for i in range(5):
    for name,text in (("short",SHORT),("long",LONG)):
        time.sleep(PAUSE); g=gpu(); dt,st,d=req(text); res[name].append(dt)
        print(name,i,st,round(dt,3),"| before: W,C,util%,MiB =",g,flush=True)
        if i==0: open(f"{OUT}-{name}.wav","wb").write(d)
for k,v in res.items(): print(k,"median",round(statistics.median(v),3),"min",round(min(v),3),"max",round(max(v),3))
