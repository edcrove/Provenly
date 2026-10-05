#!/usr/bin/env python3
"""Edge-case sweep against a running Provenly API (stdlib only).

Exercises every input class that has broken before (see docs/review.md findings):
page/offset overflow, invalid ids, NUL / invalid UTF-8 text, Content-Type, empty and
repeated query params, cross-resource ids, double submit and concurrency limits.

Usage: scripts/probe/edge_cases.py [--base http://localhost:8080]
Signs in as PROVENLY_ADMIN_USERNAME / PROVENLY_ADMIN_PASSWORD (default: the demo administrator).
Exit 1 when any request returns 5xx or a status other than the expected one.
It creates its own data (test cases prefixed "probe-"), so point it at a disposable DB.
"""
import argparse, json, os, subprocess, sys, tempfile, threading, time, urllib.error, urllib.parse, urllib.request

results = []
session = {}  # Authorization header of the signed-in administrator, sent with every call


def call(base, method, path, body=None, raw=None, ctype="application/json", headers=None):
    data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
    headers = {**({"Content-Type": ctype} if data is not None and ctype else {}), **session, **(headers or {})}
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


def oversized(url, size):
    """POST a body of size bytes with curl: a server that rejects it early closes the connection while
    urllib is still sending, but curl reads the response (Expect: 100-continue)."""
    with tempfile.NamedTemporaryFile(suffix=".xml") as f:
        f.write(b"<testsuite>" + b" " * size + b"</testsuite>")
        f.flush()
        out = subprocess.run(["curl", "-s", "-w", "\n%{http_code}", "-X", "POST", "-H", "Content-Type: application/xml",
                              "--data-binary", "@" + f.name, url], capture_output=True, text=True).stdout
    text, _, code = out.rpartition("\n")
    try:
        return int(code), json.loads(text)
    except ValueError:
        return int(code or 0), text


def check(name, got, expected):
    ok = got in (expected if isinstance(expected, tuple) else (expected,))
    results.append((ok and not (isinstance(got, int) and got >= 500), name, got, expected))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default="http://localhost:8080")
    base = ap.parse_args().base.rstrip("/") + "/api/v1"

    # Sessions (prototype feature 2): every probe below runs signed in; anonymous and broken credentials get 401.
    admin = {"username": os.environ.get("PROVENLY_ADMIN_USERNAME", "admin"), "password": os.environ.get("PROVENLY_ADMIN_PASSWORD", "provenly-demo")}
    st, body = call(base, "POST", "/auth/login", admin)
    if st != 200:
        sys.exit(f"cannot sign in as {admin['username']} ({st}): is the API up at {base}?")
    for name, hdr in [("no session", {}), ("garbage bearer", {"Authorization": "Bearer x.y.z"}), ("basic auth", {"Authorization": "Basic YWRtaW46eA=="}),
                      ("alg none", {"Authorization": "Bearer eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIiwiaXNzIjoicHJvdmVubHkifQ."}),
                      ("cookie garbage", {"Cookie": "provenly_session=%00"})]:
        check(f"GET /test-cases with {name}", call(base, "GET", "/test-cases", headers={"Authorization": "", **hdr} if hdr else {"Authorization": ""})[0], 401)
    for creds, exp in [({"username": "admin", "password": "wrong"}, 401), ({"username": "nobody-here", "password": "x" * 20}, 401),
                       ({"username": "admin\u0000", "password": "x"}, 401), ({"username": "a" * 10000, "password": "p" * 100000}, 401),
                       ({"username": "admin"}, 401), ({"user": "admin"}, 400)]:
        check(f"login {str(creds)[:40]}", call(base, "POST", "/auth/login", creds)[0], exp)
    check("login text/plain", call(base, "POST", "/auth/login", raw=b"{}", ctype="text/plain")[0], 415)
    check("accept unknown invitation", call(base, "POST", "/invitations/accept", {"token": "nope", "username": "probe_x", "displayName": "x", "password": "a long password"})[0], 404)
    check("accept with NUL", call(base, "POST", "/invitations/accept", {"token": "t\u0000", "username": "probe_x", "displayName": "x\u0000", "password": "a long password"})[0], 400)
    check("revoke id 0", call(base, "POST", "/invitations/0/revoke", headers={"Authorization": "Bearer " + body["token"]})[0], 400)
    session["Authorization"] = "Bearer " + body["token"]

    st, tc = call(base, "POST", "/test-cases", {"title": "probe-a", "automated": True})
    if st != 201:
        sys.exit(f"cannot create a test case ({st}): is the API up at {base}?")
    a, a_key = tc["id"], tc["key"]
    b = call(base, "POST", "/test-cases", {"title": "probe-b"})[1]["id"]
    sa = call(base, "POST", f"/test-cases/{a}/steps", {"action": "s1"})[1]["id"]
    sb = call(base, "POST", f"/test-cases/{b}/steps", {"action": "b1"})[1]["id"]
    xml = b'<testsuite name="probe"><testcase name="ok" time="1"/></testsuite>'
    q = f"provider=github&runId=probe{int(time.time())}x{{}}&runAttempt=1"  # unique per sweep: re-runs are not replays
    run_id = call(base, "POST", "/ingestion/junit?" + q.format(1), raw=xml, ctype="application/xml")[1]["testRun"]["id"]

    lists = ["/projects", "/test-cases", "/test-runs", f"/test-cases/{a}/steps", f"/test-cases/{a}/results",
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
    # Reports that used to be misread or silently truncated (docs/review.md findings 17, 20).
    two_roots = b'<testsuite name="a"><testcase name="r1"/></testsuite><testsuite name="b"><testcase name="r2"/></testsuite>'
    check("ingest two root elements", call(base, "POST", "/ingestion/junit?" + q.format(4), raw=two_roots, ctype="application/xml")[0], 400)
    st, body = call(base, "POST", "/ingestion/junit?" + q.format(5), raw=b'<testsuite name="s"><testcase name="t" time="1e300"/></testsuite>', ctype="application/xml")
    check("ingest absurd duration", st, 201)
    check("absurd duration is a parse error", len(body["parseErrors"]) if isinstance(body, dict) else -1, 1)
    multi = f'<testsuite name="s"><testcase name="m {a_key}"><failure message="first">d1</failure><failure message="second">d2</failure></testcase></testsuite>'
    st, body = call(base, "POST", "/ingestion/junit?" + q.format(6), raw=multi.encode(), ctype="application/xml")
    details = call(base, "GET", f"/test-runs/{body['testRun']['id']}/results")[1]["items"][0]["errorDetails"] if st == 201 else ""
    check("every failure of a testcase is kept", int("first" in details and "second" in details), 1)
    latin1 = '<?xml version="1.0" encoding="ISO-8859-1"?><testsuite name="s"><testcase name="caf\xe9"/></testsuite>'.encode("latin-1")
    check("ingest ISO-8859-1 report", call(base, "POST", "/ingestion/junit?" + q.format(7), raw=latin1, ctype="application/xml")[0], 201)
    # Ingestion media types and limits (finding 24): the charset parameter wins, compressed bodies and unknown
    # charsets are a 415, every parameter error comes at once, and an oversized report gets problem+json
    # whether the API or nginx (12 MB is past both limits) rejects it.
    no_decl = '<testsuite name="s"><testcase name="caf\xe9"/></testsuite>'.encode("latin-1")
    check("ingest charset=ISO-8859-1 header", call(base, "POST", "/ingestion/junit?" + q.format(8), raw=no_decl, ctype="application/xml; charset=ISO-8859-1")[0], 201)
    check("ingest charset=shift_jis", call(base, "POST", "/ingestion/junit?" + q.format(9), raw=b"<testsuite/>", ctype="application/xml; charset=shift_jis")[0], 415)
    check("ingest gzip body", call(base, "POST", "/ingestion/junit?" + q.format(9), raw=b"\x1f\x8b", ctype="application/xml", headers={"Content-Encoding": "gzip"})[0], 415)
    st, body = call(base, "POST", "/ingestion/junit", raw=b"<testsuite/>", ctype="application/xml")
    check("ingest without parameters lists every error", len(body.get("errors", [])) if isinstance(body, dict) else -1, 3)
    st, body = oversized(base + "/ingestion/junit?" + q.format(10), 12 * 1024 * 1024)
    check("ingest 12 MB report", st, 413)
    check("oversized report answers problem+json", body.get("code") if isinstance(body, dict) else str(body)[:40], "payload_too_large")

    # Projects (prototype feature 1): keys are validated before any lookup, unknown ones are 404s, a
    # duplicate is a 409, and test case numbers stay contiguous per project under concurrent creation.
    key = f"PR{int(time.time()) % 10**8}"  # projects are never deleted: one per sweep
    check("POST /projects", call(base, "POST", "/projects", {"key": key.lower(), "name": "probe"})[0], 201)
    check("POST /projects duplicate key", call(base, "POST", "/projects", {"key": key, "name": "again"})[0], 409)
    for bad in ["", "A", "1AB", "ABCDEFGHIJK", "A-B", "A\u0000"]:
        check(f"POST /projects key={bad!r}", call(base, "POST", "/projects", {"key": bad, "name": "x"})[0], 400)
    check("POST /projects NUL name", call(base, "POST", "/projects", {"key": "PX" + key[2:8], "name": "a\u0000"})[0], 400)
    check("POST /projects text/plain", call(base, "POST", "/projects", raw=b'{"key":"QQ","name":"x"}', ctype="text/plain")[0], 415)
    check("PATCH /projects/{key} key change", call(base, "PATCH", f"/projects/{key}", {"key": "ZZ"})[0], 400)
    check("PATCH /projects/{key} empty body", call(base, "PATCH", f"/projects/{key}", {})[0], 400)
    for path in ["/test-cases", "/test-runs"]:
        for qs, exp in [("project=", 400), (f"project={key.lower()}", 400), ("project=NOPE99", 404),
                        (f"project={key}&project=x", 200)]:
            check(f"GET {path}?{qs}", call(base, "GET", f"{path}?{qs}")[0], exp)
    for pk, exp in [("nope", 400), ("NOPE99", 404), ("%00", 400)]:
        check(f"GET /projects/{pk}", call(base, "GET", f"/projects/{pk}")[0], exp)
    check("ingest project=", call(base, "POST", "/ingestion/junit?" + q.format(11) + "&project=", raw=xml, ctype="application/xml")[0], 400)
    check("ingest unknown project", call(base, "POST", "/ingestion/junit?" + q.format(11) + "&project=NOPE99", raw=xml, ctype="application/xml")[0], 404)
    check("create test case in unknown project", call(base, "POST", "/test-cases", {"title": "x", "project": "NOPE99"})[0], 404)
    numbers = []
    threads = [threading.Thread(target=lambda: numbers.append(call(base, "POST", "/test-cases", {"title": "probe-p", "project": key})[1].get("number"))) for _ in range(30)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("30 parallel creations in a project: contiguous numbers", int(sorted(numbers) == list(range(1, 31))), 1)

    # Roles (prototype feature 3): a viewer of one project sees only it, gets 403 on writes there and 404 elsewhere;
    # member routes validate keys, usernames and roles before any lookup.
    st, inv = call(base, "POST", "/invitations", {"project": key, "role": "viewer"})
    check("invite into a project", st, 201)
    viewer_name = f"probe.v{int(time.time()) % 10**8}"
    st, sess = call(base, "POST", "/invitations/accept", {"token": inv.get("token", ""), "username": viewer_name, "displayName": "Probe viewer", "password": "a long password"}, headers={"Authorization": ""})
    check("accept a project invitation", st, 201)
    as_viewer = {"Authorization": "Bearer " + (sess.get("token", "") if isinstance(sess, dict) else "")}
    st, projects = call(base, "GET", "/projects", headers=as_viewer)
    check("viewer lists only their project", [(p["key"], p["myRole"]) for p in projects.get("items", [])] if isinstance(projects, dict) else st, [(key, "viewer")])
    mine = call(base, "POST", "/test-cases", {"title": "probe-r", "project": key})[1]["id"]
    for method, path, body, exp in [("GET", f"/test-cases/{mine}", None, 200), ("GET", f"/test-cases/{a}", None, 404),
                                    ("GET", f"/test-runs/{run_id}", None, 404), ("GET", f"/test-cases/{a}/results", None, 404),
                                    ("PATCH", f"/test-cases/{mine}", {"title": "x"}, 403), ("POST", f"/test-cases/{mine}/deprecate", None, 403),
                                    ("POST", f"/test-cases/{mine}/steps", {"action": "x"}, 403), ("POST", "/test-cases", {"title": "x", "project": key}, 403),
                                    ("POST", "/test-cases", {"title": "x"}, 404), ("POST", "/projects", {"key": "QV", "name": "x"}, 403),
                                    ("PATCH", f"/projects/{key}", {"name": "x"}, 403), ("GET", f"/projects/{key}/members", None, 200),
                                    ("PUT", f"/projects/{key}/members/{viewer_name}", {"role": "maintainer"}, 403),
                                    ("GET", "/projects/NOPE99/members", None, 404), ("GET", "/users", None, 403), ("POST", "/invitations", {}, 403)]:
        check(f"viewer {method} {path}", call(base, method, path, body, headers=as_viewer)[0], exp)
    for user, body, exp in [(viewer_name, {"role": "owner"}, 400), (viewer_name, {"role": ""}, 400), (viewer_name, {}, 400),
                            ("nobody-here", {"role": "member"}, 404), ("%00", {"role": "member"}, 400), ("a" * 300, {"role": "member"}, 404)]:
        check(f"PUT member {user[:20]!r} {body}", call(base, "PUT", f"/projects/{key}/members/{user}", body)[0], exp)
    check("PUT member text/plain", call(base, "PUT", f"/projects/{key}/members/{viewer_name}", raw=b'{"role":"member"}', ctype="text/plain")[0], 415)
    check("PUT member bad key", call(base, "PUT", f"/projects/bad/members/{viewer_name}", {"role": "member"})[0], 400)
    check("invite role without project", call(base, "POST", "/invitations", {"role": "viewer"})[0], 400)
    check("invite unknown project", call(base, "POST", "/invitations", {"project": "NOPE99", "role": "viewer"})[0], 404)
    check("promote to member", call(base, "PUT", f"/projects/{key}/members/{viewer_name}", {"role": "member"})[0], 200)
    check("member edits", call(base, "PATCH", f"/test-cases/{mine}", {"title": "probe-r2"}, headers=as_viewer)[0], 200)
    check("remove member", call(base, "DELETE", f"/projects/{key}/members/{viewer_name}")[0], 204)
    check("removed member: project invisible", call(base, "GET", f"/test-cases/{mine}", headers=as_viewer)[0], 404)
    check("remove again", call(base, "DELETE", f"/projects/{key}/members/{viewer_name}")[0], 404)

    # API keys (prototype feature 4): malformed, unknown and revoked keys are 401s; a key reports into its project
    # only and opens no other route; names are validated; ids and keys never reach the database malformed.
    st, created = call(base, "POST", f"/projects/{key}/api-keys", {"name": "probe ci"})
    check("create an API key", st, 201)
    token = created.get("token", "") if isinstance(created, dict) else ""
    as_key = {"Authorization": "Bearer " + token}
    ingest_q = "/ingestion/junit?" + q.format(20)
    for name, hdr in [("no key", {"Authorization": ""}), ("pvk_ alone", {"Authorization": "Bearer pvk_"}),
                      ("truncated key", {"Authorization": "Bearer " + token[:-1]}), ("key + NUL", {"Authorization": "Bearer " + token[:-1] + "%00"}),
                      ("huge key", {"Authorization": "Bearer pvk_" + "a" * 4000}), ("key as cookie", {"Authorization": "", "Cookie": "provenly_session=" + token})]:
        check(f"ingest with {name}", call(base, "POST", ingest_q, raw=xml, ctype="application/xml", headers=hdr)[0], 401)
    check("ingest with a key", call(base, "POST", ingest_q, raw=xml, ctype="application/xml", headers=as_key)[0], 201)
    check("key into another project", call(base, "POST", "/ingestion/junit?" + q.format(21) + "&project=TC", raw=xml, ctype="application/xml", headers=as_key)[0], 404)
    for method, path in [("GET", "/test-runs"), ("GET", "/auth/me"), ("GET", f"/projects/{key}/api-keys"), ("POST", "/test-cases")]:
        check(f"key on {method} {path}", call(base, method, path, {} if method == "POST" else None, headers=as_key)[0], 401)
    for body, exp in [({"name": ""}, 400), ({"name": "  "}, 400), ({"name": "n" * 101}, 400), ({"name": "a\u0000"}, 400), ({}, 400), ({"name": "ñ" * 100}, 201)]:
        check(f"create key {str(body)[:30]}", call(base, "POST", f"/projects/{key}/api-keys", body)[0], exp)
    check("create key text/plain", call(base, "POST", f"/projects/{key}/api-keys", raw=b'{"name":"x"}', ctype="text/plain")[0], 415)
    for kid, exp in [("0", 400), ("-1", 400), ("abc", 400), ("9223372036854775808", 400), ("9223372036854775807", 404)]:
        check(f"revoke key {kid}", call(base, "POST", f"/projects/{key}/api-keys/{kid}/revoke")[0], exp)
    check("viewer lists keys", call(base, "GET", f"/projects/{key}/api-keys", headers=as_viewer)[0], 404)
    kid = created.get("apiKey", {}).get("id", 0) if isinstance(created, dict) else 0
    check("revoke the key", call(base, "POST", f"/projects/{key}/api-keys/{kid}/revoke")[0], 200)
    check("revoke it again", call(base, "POST", f"/projects/{key}/api-keys/{kid}/revoke")[0], 409)
    check("revoked key", call(base, "POST", "/ingestion/junit?" + q.format(22), raw=xml, ctype="application/xml", headers=as_key)[0], 401)

    # Optimistic locking (prototype feature 5): malformed If-Match is a 400, a stale or weak tag a 412 that changes
    # nothing, "*" and the current tag pass; 20 concurrent writes from the same read: exactly one wins.
    v = call(base, "GET", f"/test-cases/{b}")[1]["version"]
    for hdr, exp in [("7", 400), ('"7', 400), ("'7'", 400), ('"a b"', 400), ('"7",', 400), ('*, "7"', 400), ("W/" + '"' + str(v) + '"', 412),
                     ('"0"', 412), ('"99999999999999999999"', 412), (", ".join(f'"{i}"' for i in range(500)) if v >= 500 else ", ".join(f'"{i + 1000}"' for i in range(500)), 412)]:
        check(f"If-Match {hdr[:20]!r}", call(base, "PATCH", f"/test-cases/{b}", {"title": "probe-b"}, headers={"If-Match": hdr})[0], exp)
    check("If-Match stale left the title", call(base, "GET", f"/test-cases/{b}")[1]["title"], "probe-b")
    check("If-Match *", call(base, "PATCH", f"/test-cases/{b}", {"title": "probe-b2"}, headers={"If-Match": "*"})[0], 200)
    st, cur = call(base, "GET", f"/test-cases/{b}")
    for path, method, body in [(f"/test-cases/{b}/steps", "POST", {"action": "x"}), (f"/test-cases/{b}/steps/order", "PUT", {"stepIds": [sb]}),
                               (f"/test-cases/{b}/steps/{sb}", "PATCH", {"action": "y"}), (f"/test-cases/{b}/steps/{sb}", "DELETE", None),
                               (f"/test-cases/{b}/deprecate", "POST", None), (f"/test-cases/{b}/reactivate", "POST", None)]:
        check(f"{method} {path} stale", call(base, method, path, body, headers={"If-Match": '"1"'})[0], 412)
    tag = '"' + str(cur["version"]) + '"'
    codes = []
    threads = [threading.Thread(target=lambda i=i: codes.append(call(base, "PATCH", f"/test-cases/{b}", {"title": f"probe-race-{i}"}, headers={"If-Match": tag})[0])) for i in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent saves from one read: one wins", f"{codes.count(200)} won, {codes.count(412)} refused", "1 won, 19 refused")

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
