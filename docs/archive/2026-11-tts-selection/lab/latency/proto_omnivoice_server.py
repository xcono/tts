"""Same latency protocol for the OmniVoice Python server (pcm, time to first byte and to full response)."""
import http.client, json, subprocess, sys, time, statistics
PORT=int(sys.argv[1]); PAUSE=15
SHORT="Тесты прошли, изменения закоммичены, pull request открыт."
LONG="Репозиторий лежит в корневой папке, все Go-файлы целы и не изменены, ветка main, последний коммит прежний."
def gpu():
    return subprocess.run(["nvidia-smi","-i","1","--query-gpu=power.draw,temperature.gpu,utilization.gpu,memory.used","--format=csv,noheader,nounits"],capture_output=True,text=True).stdout.strip()
def req(text):
    b={"model":"tts","input":text,"response_format":"pcm"}
    c=http.client.HTTPConnection("127.0.0.1",PORT,timeout=300)
    t=time.perf_counter(); c.request("POST","/v1/audio/speech",json.dumps(b,ensure_ascii=False).encode(),{"Content-Type":"application/json"})
    r=c.getresponse(); first=None; n=0
    while True:
        x=r.read1(65536)
        if not x: break
        if first is None: first=time.perf_counter()-t
        n+=len(x)
    dt=time.perf_counter()-t; c.close(); return first,dt,r.status,n/48000
for name,text in (("warm",SHORT),("warm",LONG)):
    f,dt,st,dur=req(text); print(name,st,round(dt,3),flush=True)
    if st!=200: sys.exit(1)
res={"short":[],"long":[]}
for i in range(5):
    for name,text in (("short",SHORT),("long",LONG)):
        time.sleep(PAUSE); g=gpu(); f,dt,st,dur=req(text); res[name].append(dt)
        print(name,i,st,"first",round(f,3),"full",round(dt,3),"audio",round(dur,2),"s | before: W,C,util%,MiB =",g,flush=True)
for k,v in res.items(): print(k,"median",round(statistics.median(v),3),"min",round(min(v),3),"max",round(max(v),3))
