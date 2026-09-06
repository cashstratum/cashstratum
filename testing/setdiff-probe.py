# Count mining.set_difficulty frames between subscribe and the authorize result.
#
# WHY THIS EXISTS: nothing in test/ or regtest-e2e.sh asserts on set_difficulty
# COUNT or ordering, so a protocol regression there is invisible to the gate.
# This probe is what measured 3 -> 2 for the fix in PR #20.
#
# The useragent is self-identifying ON PURPOSE. A probe sent with a spoofed
# MiningRigRentals useragent poisoned a production sharelog corpus and was twice
# mistaken for real marketplace traffic, producing a confident false conclusion.
# Never point this at production with a borrowed useragent.
import socket, json, time
HOST, PORT = "solo.blocksniper.ai", 3333
USER = "bitcoincash:qqqupxkkrjew738czfzpz5e33sej6wm9zqdquq0aze"
UA = "claude-code-diagnostic-probe/1.0"
s = socket.create_connection((HOST, PORT), timeout=10); s.settimeout(10)
f = s.makefile("rwb")
def send(o): f.write((json.dumps(o)+"\n").encode()); f.flush()
send({"id":1,"method":"mining.subscribe","params":[UA]})
send({"id":2,"method":"mining.authorize","params":[USER,"d=20000"]})
t0=time.time(); n_diff=0
while time.time()-t0 < 9:
    try: line=f.readline()
    except socket.timeout: break
    if not line: break
    try: m=json.loads(line)
    except Exception: continue
    meth=m.get("method"); 
    if meth=="mining.set_difficulty":
        n_diff+=1
        print("  [%+.2fs] set_difficulty %s   <-- #%d" % (time.time()-t0, m["params"], n_diff))
    elif meth=="mining.notify":
        print("  [%+.2fs] mining.notify (job %s)" % (time.time()-t0, m["params"][0]))
    elif m.get("id")==2:
        print("  [%+.2fs] AUTHORIZE RESULT: %s" % (time.time()-t0, m.get("result")))
    elif m.get("id")==1:
        print("  [%+.2fs] subscribe ok" % (time.time()-t0))
s.close()
print("\nTOTAL mining.set_difficulty in 9s on ONE authorize: %d" % n_diff)
