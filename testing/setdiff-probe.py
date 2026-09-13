# Count mining.set_difficulty frames between subscribe and the authorize result.
#
# WHY THIS EXISTS: nothing in test/ or regtest-e2e.sh asserts on set_difficulty
# COUNT or ordering, so a protocol regression there is invisible to the gate.
# This probe is what measured 3 -> 2 for the fix in PR #20.
#
# The useragent is self-identifying ON PURPOSE. A probe sent with a spoofed
# MiningRigRentals useragent poisoned a production sharelog corpus and was twice
# mistaken for real marketplace traffic, producing a confident false conclusion.
# Never point this at a live pool with a borrowed useragent.
#
# The target is a required argument and has no default: pointing a diagnostic
# probe at a pool you did not mean to touch must take a deliberate keystroke.
#
#   usage: setdiff-probe.py <host> <port> <payout-address>
import socket, json, time, sys
if len(sys.argv) != 4:
    sys.exit("usage: %s <host> <port> <payout-address>" % sys.argv[0])
HOST, PORT, USER = sys.argv[1], int(sys.argv[2]), sys.argv[3]
UA = "cashstratum-setdiff-probe/1.0"
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
