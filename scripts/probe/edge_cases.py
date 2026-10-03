#!/usr/bin/env python3
"""Edge-case sweep against a running Provenly API (stdlib only).

Exercises every input class that has broken before (see docs/review.md findings):
page/offset overflow, invalid ids, NUL / invalid UTF-8 text, Content-Type, empty and
repeated query params, cross-resource ids, double submit and concurrency limits.

Usage: scripts/probe/edge_cases.py [--base http://localhost:8080]
Exit 1 when any request returns 5xx or a status other than the expected one.
It creates its own data (test cases prefixed "probe-"), so point it at a disposable DB.
"""
import argparse, json, sys, threading, urllib.error, urllib.parse, urllib.request

results = []


def call(base, method, path, body=None, raw=None, ctype="application/json"):
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    headers = {"Content-Type": ctype} if data is not None and ctype else {}
    req = urllib.request.Request(base + path, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            text = r.read().decode()
            return r.status, (json.loads(text) if text else None)
    except urllib.error.HTTPError as e:
        text = e.read().decode()
        try:
            return e.code, json.loads(text)
        except ValueError:
            return e.code, text


def check(name, got, expected):
    ok = got in (expected if isinstance(expected, tuple) else (expected,))
    results.append((ok and got < 500, name, got, expected))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    base = ap.parse_args().base.rstrip("/") + "/api/v1"

    st, tc = call(base, "POST", "/test-cases", {"title": "probe-a", "automated": True})
    if st != 201:
        sys.exit(f"cannot create a test case ({st}): is the API up at {base}?")
    a = tc["id"]
    b = call(base, "POST", "/test-cases", {"title": "probe-b"})[1]["id"]
    sa = call(base, "POST", f"/test-cases/{a}/steps", {"action": "s1"})[1]["id"]
    sb = call(base, "POST", f"/test-cases/{b}/steps", {"action": "b1"})[1]["id"]
    xml = b'<testsuite name="probe"><testcase name="ok" time="1"/></testsuite>'
    q = "provider=github&runId=probe{}&runAttempt=1"
    run_id = call(base, "POST", "/ingestion/junit?" + q.format(1), raw=xml, ctype="application/xml")[1]["testRun"]["id"]

    lists = ["/test-cases", "/test-runs", f"/test-cases/{a}/steps", f"/test-cases/{a}/results",
             f"/test-runs/{run_id}/results", f"/test-runs/{run_id}/parse-errors"]
    for path in lists:
        for qs, exp in [("page=21474838&pageSize=100", 400), ("page=0", 400), ("page=", 400), ("pageSize=101", 400),
                        ("page=1.5", 400), ("page=abc", 400), ("page=999", 200), ("page=2&page=x", 200),
                        ("unknown=1", 200), ("pageSize=", 400)]:
            check(f"GET {path}?{qs}", call(base, "GET", f"{path}?{qs}")[0], exp)
    check("status= (empty enum)", call(base, "GET", "/test-cases?status=")[0], 400)
    check("status=bogus", call(base, "GET", "/test-cases?status=bogus")[0], 400)

    for pid, exp in [("0", 400), ("-1", 400), ("abc", 400), ("1.5", 400), ("9223372036854775808", 400),
                     ("9223372036854775807", 404), ("%00", 400)]:
        check(f"GET /test-cases/{pid}", call(base, "GET", f"/test-cases/{pid}")[0], exp)
        check(f"GET /test-runs/{pid}", call(base, "GET", f"/test-runs/{pid}")[0], exp)

    for field in ["title", "description", "expectedResult"]:
        body = {"title": "probe"}
        body[field] = "a\u0000b"
        check(f"POST /test-cases {field} with NUL", call(base, "POST", "/test-cases", body)[0], 400)
    check("POST step action with NUL", call(base, "POST", f"/test-cases/{a}/steps", {"action": "\u0000"})[0], 400)
    check("PATCH step with NUL", call(base, "PATCH", f"/test-cases/{a}/steps/{sa}", {"expectedResult": "\u0000"})[0], 400)
    check("title whitespace only", call(base, "POST", "/test-cases", {"title": "   "})[0], 400)
    check("title 201 chars", call(base, "POST", "/test-cases", {"title": "t" * 201})[0], 400)
    check("title 200 emoji", call(base, "POST", "/test-cases", {"title": "\U0001F600" * 200})[0], 201)
    check("client-sent id rejected", call(base, "POST", "/test-cases", {"title": "x", "id": 5})[0], 400)
    check("duplicate JSON keys", call(base, "POST", "/test-cases", raw=b'{"title":"a","title":"probe-dup"}')[0], 201)
    check("invalid JSON", call(base, "POST", "/test-cases", raw=b'{"title":')[0], 400)
    check("text/plain body", call(base, "POST", "/test-cases", raw=b'{"title":"x"}', ctype="text/plain")[0], 415)
    check("no Content-Type", call(base, "POST", "/test-cases", raw=b'{"title":"x"}', ctype=None)[0], 415)

    check("PATCH other TC's step", call(base, "PATCH", f"/test-cases/{a}/steps/{sb}", {"action": "x"})[0], 404)
    check("DELETE other TC's step", call(base, "DELETE", f"/test-cases/{a}/steps/{sb}")[0], 404)
    check("reorder with foreign step", call(base, "PUT", f"/test-cases/{a}/steps/order", {"stepIds": [sa, sb]})[0], 400)
    check("reorder with duplicates", call(base, "PUT", f"/test-cases/{a}/steps/order", {"stepIds": [sa, sa]})[0], 400)

    for k, v, exp in [("branch", "ma%00in", 400), ("pipeline", "%FF", 400), ("commit", "c%00", 400),
                      ("status", "", 400), ("status", "failed", 400), ("status", "running", 400), ("runAttempt", "0", 400)]:
        # A repeated param keeps its first value, so override runAttempt in place instead of appending it.
        qs = q.format(2).replace("runAttempt=1", f"runAttempt={v}") if k == "runAttempt" else q.format(2) + f"&{k}={v}"
        check(f"ingest {k}={v}", call(base, "POST", "/ingestion/junit?" + qs, raw=xml, ctype="application/xml")[0], exp)
    check("ingest NUL in XML", call(base, "POST", "/ingestion/junit?" + q.format(3), raw=b'<testsuite><testcase name="a&#0;"/></testsuite>', ctype="application/xml")[0], 400)
    check("ingest not XML", call(base, "POST", "/ingestion/junit?" + q.format(3), raw=b"<nope", ctype="application/xml")[0], 400)
    check("ingest JSON body", call(base, "POST", "/ingestion/junit?" + q.format(3), body={})[0], 415)

    # Concurrency: 110 parallel step creations on a fresh TC -> exactly 100 created, positions 1..100.
    c = call(base, "POST", "/test-cases", {"title": "probe-c"})[1]["id"]
    codes = []
    threads = [threading.Thread(target=lambda i=i: codes.append(call(base, "POST", f"/test-cases/{c}/steps", {"action": f"c{i}"})[0])) for i in range(110)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("110 parallel steps: 201 count", codes.count(201), 100)
    positions = sorted(s["position"] for s in call(base, "GET", f"/test-cases/{c}/steps?pageSize=100")[1]["items"])
    check("110 parallel steps: contiguous positions", int(positions == list(range(1, 101))), 1)

    failed = [r for r in results if not r[0]]
    for ok, name, got, exp in results:
        if not ok:
            print(f"FAIL  {name}: got {got}, expected {exp}")
    print(f"{len(results) - len(failed)}/{len(results)} checks passed")
    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
