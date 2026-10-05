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
import argparse, gzip, json, os, subprocess, sys, tempfile, threading, time, urllib.error, urllib.parse, urllib.request

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
    # gzip (prototype feature 8, D6): the limit applies to the decompressed report; broken streams are 400s.
    gz = lambda b: gzip.compress(b)
    check("ingest broken gzip", call(base, "POST", "/ingestion/junit?" + q.format(9), raw=b"\x1f\x8b", ctype="application/xml", headers={"Content-Encoding": "gzip"})[0], 400)
    check("ingest gzip report", call(base, "POST", "/ingestion/junit?" + q.format(50), raw=gz(b'<testsuite><testcase name="gz"/></testsuite>'), ctype="application/xml", headers={"Content-Encoding": "gzip"})[0], 201)
    check("ingest x-gzip report", call(base, "POST", "/ingestion/junit?" + q.format(51), raw=gz(b'<testsuite/>'), ctype="application/xml", headers={"Content-Encoding": "x-gzip"})[0], 201)
    check("ingest gzip bomb (64 MB of spaces)", call(base, "POST", "/ingestion/junit?" + q.format(52), raw=gz(b" " * (64 << 20)), ctype="application/xml", headers={"Content-Encoding": "gzip"})[0], 413)
    check("ingest truncated gzip", call(base, "POST", "/ingestion/junit?" + q.format(53), raw=gz(b'<testsuite/>')[:15], ctype="application/xml", headers={"Content-Encoding": "gzip"})[0], 400)
    check("ingest br body", call(base, "POST", "/ingestion/junit?" + q.format(9), raw=b"<testsuite/>", ctype="application/xml", headers={"Content-Encoding": "br"})[0], 415)
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

    # Snapshot amendment (prototype feature 6, DEC-42): only reported TC-IDs outside the snapshot, a reason, once.
    manual = call(base, "POST", "/test-cases", {"title": "probe-manual"})[1]
    rid = call(base, "POST", "/ingestion/junit?" + q.format(30), raw=f'<testsuite><testcase name="m {manual["key"]}"/></testsuite>'.encode(), ctype="application/xml")[1]["testRun"]["id"]
    amend = f"/test-runs/{rid}/amendments"
    for body, exp in [({"testCaseId": 0, "reason": "x"}, 400), ({"testCaseId": -1, "reason": "x"}, 400), ({"testCaseId": "1", "reason": "x"}, 400),
                      ({"testCaseId": 9223372036854775807, "reason": "x"}, 400), ({"testCaseId": 1e30, "reason": "x"}, 400), ({"reason": "x"}, 400),
                      ({"testCaseId": manual["id"]}, 400), ({"testCaseId": manual["id"], "reason": "a\u0000"}, 400),
                      ({"testCaseId": manual["id"], "reason": "r" * 501}, 400), ({"testCaseId": a, "reason": "x"}, 409)]:
        check(f"amend {str(body)[:40]}", call(base, "POST", amend, body)[0], exp)
    check("amend text/plain", call(base, "POST", amend, raw=b'{}', ctype="text/plain")[0], 415)
    for rq, exp in [("0", 400), ("abc", 400), ("9223372036854775807", 404)]:
        check(f"amend run {rq}", call(base, "POST", f"/test-runs/{rq}/amendments", {"testCaseId": manual["id"], "reason": "x"})[0], exp)
        check(f"amendments of run {rq}", call(base, "GET", f"/test-runs/{rq}/amendments")[0], exp)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", amend, {"testCaseId": manual["id"], "reason": "probe race"})[0])) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent amendments: one recorded", f"{codes.count(201)} recorded, {codes.count(409)} conflicts", "1 recorded, 19 conflicts")
    check("amended run is marked", call(base, "GET", f"/test-runs/{rid}")[1].get("amendmentCount"), 1)

    # Retries (prototype feature 7, D1): attempt/retry properties out of range are kept as first attempts with a
    # warning; Surefire flaky elements become attempts; a huge rerun list is capped at 100 attempts.
    def attempts(props, inner=""):
        doc = f'<testsuite name="s"><testcase name="probe-retry"><properties>{props}</properties>{inner}</testcase></testsuite>'
        st, body = call(base, "POST", "/ingestion/junit?" + q.format(40 + len(results)), raw=doc.encode(), ctype="application/xml")
        return st, body
    for props, warn in [('<property name="attempt" value="0"/>', True), ('<property name="attempt" value="-5"/>', True),
                        ('<property name="retry" value="99999999999999999999"/>', True), ('<property name="attempt" value="1e2"/>', True),
                        ('<property name="attempt" value="100"/>', False), ('<property name="retry" value="99"/>', False)]:
        st, body = attempts(props)
        check(f"ingest {props[16:50]}", st, 201)
        check(f"{props[16:50]} warns", int(bool(isinstance(body, dict) and body.get("parseErrors"))), int(warn))
    st, body = attempts("", '<failure message="x"/>' + '<rerunFailure message="again"/>' * 150)
    check("150 reruns: capped", (st, body.get("persisted") if isinstance(body, dict) else None) == (201, 100), True)
    st, body = attempts("", '<flakyFailure message="a"/><flakyError message="b"/>')
    check("flaky elements are attempts", body.get("persisted") if isinstance(body, dict) else st, 3)

    # Taxonomy (prototype feature 9): keys and names are validated before any lookup, duplicates are 409s, tags are
    # normalized and bounded, classification values must exist and be active, list filters are validated; concurrent
    # creations of one key: exactly one wins.
    dims = f"/projects/{key}/dimensions"
    st, listed = call(base, "GET", dims)
    check("list dimensions", f"{st} {len(listed.get('items', [])) if isinstance(listed, dict) else 0}", "200 7")
    for path, exp in [("/projects/bad/dimensions", 400), ("/projects/NOPE99/dimensions", 404)]:
        check(f"GET {path}", call(base, "GET", path)[0], exp)
    for body, exp in [({"key": "Browser", "name": "x"}, 400), ({"key": "1x", "name": "x"}, 400), ({"key": "b" * 31, "name": "x"}, 400),
                      ({"key": "x", "name": ""}, 400), ({"key": "x", "name": "n" * 61}, 400), ({"key": "x", "name": "a\u0000"}, 400),
                      ({"key": "x"}, 400), ({"key": 1, "name": "x"}, 400), ({"key": "risk", "name": "again"}, 409), ({"key": "probe-dim", "name": "ñ" * 60}, 201)]:
        check(f"create dimension {str(body)[:40]}", call(base, "POST", dims, body)[0], exp)
    check("create dimension text/plain", call(base, "POST", dims, raw=b'{"key":"y","name":"y"}', ctype="text/plain")[0], 415)
    for path, body, exp in [(f"{dims}/Risk", {"name": "x"}, 400), (f"{dims}/nope", {"name": "x"}, 404), (f"{dims}/risk", {}, 400),
                            (f"{dims}/risk", {"archived": "yes"}, 400), (f"{dims}/risk/values/Low", {"name": "x"}, 400),
                            (f"{dims}/risk/values/nope", {"name": "x"}, 404), (f"{dims}/nope/values/low", {"name": "x"}, 404),
                            (f"{dims}/risk/values/low", {"name": " "}, 400), (f"{dims}/%00/values/low", {"name": "x"}, 400)]:
        check(f"PATCH {path[len(dims):]} {body}", call(base, "PATCH", path, body)[0], exp)
    for body, exp in [({"key": "-x", "name": "x"}, 400), ({"key": "low", "name": "again"}, 409), ({"key": "probe-v", "name": "V"}, 201)]:
        check(f"add value {body}", call(base, "POST", f"{dims}/risk/values", body)[0], exp)
    check("add value to unknown dimension", call(base, "POST", f"{dims}/nope/values", {"key": "x", "name": "x"})[0], 404)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", dims, {"key": "probe-race", "name": "race"})[0])) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent dimension creations: one wins", f"{codes.count(201)} created, {codes.count(409)} conflicts", "1 created, 19 conflicts")
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", f"{dims}/risk/values", {"key": "probe-race", "name": "race"})[0])) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent value creations: one wins", f"{codes.count(201)} created, {codes.count(409)} conflicts", "1 created, 19 conflicts")
    many = [f"t{i}" for i in range(20)]
    for body, exp in [({"tags": many + ["T0", " t1 "]}, 201), ({"tags": many + ["t20"]}, 400), ({"tags": ["a" * 41]}, 400),
                      ({"tags": ["a\u0000"]}, 400), ({"tags": ["ñ"]}, 400), ({"tags": "smoke"}, 400), ({"tags": [1]}, 400),
                      ({"classification": {"risk": "critical"}}, 201), ({"classification": {"risk": None}}, 400),
                      ({"classification": {"nope": "x"}}, 400), ({"classification": {"risk": "nope"}}, 400),
                      ({"classification": {"risk": 1}}, 400), ({"classification": ["risk"]}, 400), ({"classification": {"%00": "x"}}, 400)]:
        check(f"create test case {str(body)[:50]}", call(base, "POST", "/test-cases", {"title": "probe-tax", "project": key, **body})[0], exp)
    check("archive a value", call(base, "PATCH", f"{dims}/risk/values/probe-v", {"archived": True})[0], 200)
    check("assign an archived value", call(base, "POST", "/test-cases", {"title": "probe-tax", "project": key, "classification": {"risk": "probe-v"}})[0], 400)
    for qs, exp in [("tag=t0", 200), ("tag=", 400), ("tag=T0", 400), ("tag=%00", 400), ("tag=" + "a" * 41, 400),
                    ("classification=risk:critical", 200), ("classification=risk:critical,risk:critical", 200), ("classification=risk", 400),
                    ("classification=", 400), ("classification=" + ",".join(f"d{i}:v" for i in range(11)), 400), ("classification=%C3%B1:x", 400)]:
        check(f"list ?{qs[:40]}", call(base, "GET", f"/test-cases?project={key}&{qs}")[0], exp)
    st, page = call(base, "GET", f"/test-cases?project={key}&tag=t0")
    check("tag filter finds the normalized tags", page.get("totalItems") if isinstance(page, dict) else st, 1)

    # Suites (prototype feature 10, MVP D2): keys, kinds, queries and member lists are validated before any lookup;
    # duplicates are 409s; ?suite= on ingestion, test case and run lists is validated; archived suites take no runs;
    # concurrent creations of one key: exactly one wins.
    suites = f"/projects/{key}/suites"
    member = call(base, "POST", "/test-cases", {"title": "probe-suite", "project": key, "automated": True, "tags": ["probe-s"]})[1]["id"]
    for body, exp in [({"key": "Bad", "name": "x", "kind": "static"}, 400), ({"key": "x", "name": "", "kind": "static"}, 400),
                      ({"key": "x", "name": "x", "kind": "dynamic"}, 400), ({"key": "x", "name": "x"}, 400),
                      ({"key": "x", "name": "x", "kind": "query"}, 400), ({"key": "x", "name": "x", "kind": "query", "query": {"tag": "A B"}}, 400),
                      ({"key": "x", "name": "x", "kind": "query", "query": {"classification": ["risk"]}}, 400),
                      ({"key": "x", "name": "x", "kind": "query", "query": {"classification": [f"d{i}:v" for i in range(11)]}}, 400),
                      ({"key": "x", "name": "x", "kind": "static", "query": {"tag": "a"}}, 400),
                      ({"key": "x", "name": "x", "kind": "static", "testCaseIds": [0]}, 400), ({"key": "x", "name": "x", "kind": "static", "testCaseIds": [-1]}, 400),
                      ({"key": "x", "name": "x", "kind": "static", "testCaseIds": [9223372036854775807]}, 400),
                      ({"key": "x", "name": "x", "kind": "static", "testCaseIds": [1.5]}, 400), ({"key": "x", "name": "x", "kind": "static", "testCaseIds": list(range(1, 1002))}, 400),
                      ({"key": "x", "name": "a\u0000", "kind": "static"}, 400), ({"key": "x", "name": "x", "kind": "static", "description": "d" * 2001}, 400),
                      ({"key": "probe-static", "name": "ñ" * 100, "kind": "static", "testCaseIds": [member, member]}, 201),
                      ({"key": "probe-query", "name": "Q", "kind": "query", "query": {"tag": "Probe-S"}}, 201),
                      ({"key": "probe-static", "name": "again", "kind": "static"}, 409)]:
        check(f"create suite {str(body)[:50]}", call(base, "POST", suites, body)[0], exp)
    check("create suite text/plain", call(base, "POST", suites, raw=b"{}", ctype="text/plain")[0], 415)
    for path, method, body, exp in [(f"{suites}/Bad", "GET", None, 400), (f"{suites}/nope", "GET", None, 404), (f"{suites}/%00", "GET", None, 400),
                                    (f"{suites}/probe-static", "PATCH", {}, 400), (f"{suites}/probe-static", "PATCH", {"query": {"tag": "x"}}, 400),
                                    (f"{suites}/probe-query", "PATCH", {"query": {}}, 400), (f"{suites}/probe-query", "PATCH", {"archived": "yes"}, 400),
                                    (f"{suites}/probe-static/cases", "PUT", {}, 400), (f"{suites}/probe-static/cases", "PUT", {"testCaseIds": None}, 400),
                                    (f"{suites}/probe-query/cases", "PUT", {"testCaseIds": []}, 400), (f"{suites}/probe-static/cases", "PUT", {"testCaseIds": [a]}, 400),
                                    (f"{suites}/nope/cases", "PUT", {"testCaseIds": []}, 404), (f"{suites}/probe-static/cases", "PUT", {"testCaseIds": [member]}, 200)]:
        check(f"{method} {path[len(suites):] or '/'} {str(body)[:30]}", call(base, method, path, body)[0], exp)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", suites, {"key": "probe-race", "name": "race", "kind": "static"})[0])) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent suite creations: one wins", f"{codes.count(201)} created, {codes.count(409)} conflicts", "1 created, 19 conflicts")
    for qs, exp in [(f"project={key}&suite=probe-static", 200), ("suite=probe-static", 400), (f"project={key}&suite=", 400), (f"project={key}&suite=Bad", 400),
                    (f"project={key}&suite=nope", 404), (f"project={key}&suite=probe-static&tag=x", 400)]:
        check(f"test cases ?{qs[-40:]}", call(base, "GET", f"/test-cases?{qs}")[0], exp)
    for qs, exp in [("suite=probe-static", 200), ("suite=", 400), ("suite=Bad", 400), ("suite=%00", 400)]:
        check(f"runs ?{qs}", call(base, "GET", f"/test-runs?{qs}")[0], exp)
    sq = "/ingestion/junit?project=" + key + "&" + q.format(60)
    for suite, exp in [("&suite=", 400), ("&suite=Bad", 400), ("&suite=nope", 404), ("&suite=" + "s" * 31, 400)]:
        check(f"ingest {suite}", call(base, "POST", sq + suite, raw=xml, ctype="application/xml")[0], exp)
    st, body = call(base, "POST", sq + "&suite=probe-query", raw=xml, ctype="application/xml")
    check("ingest for a suite", f"{st} expected {body.get('testRun', {}).get('expectedCount') if isinstance(body, dict) else None}", "201 expected 1")
    check("archive a suite", call(base, "PATCH", f"{suites}/probe-query", {"archived": True})[0], 200)
    check("ingest for an archived suite", call(base, "POST", "/ingestion/junit?project=" + key + "&suite=probe-query&" + q.format(61), raw=xml, ctype="application/xml")[0], 409)

    # Manual execution (prototype feature 11, MVP D3): inputs are validated before any lookup; only running manual
    # runs take results, only for expected test cases; finishing closes the run; twenty concurrent records of one test
    # case are twenty gapless attempts.
    hand = call(base, "POST", "/test-cases", {"title": "probe-manual-run", "project": key})[1]["id"]
    for body, exp in [({"project": key}, 400), ({"project": key, "name": " "}, 400), ({"project": key, "name": "n" * 201}, 400),
                      ({"project": key, "name": "x", "scope": "some"}, 400), ({"project": key, "name": "x", "suite": "Bad"}, 400),
                      ({"project": "bad", "name": "x"}, 400), ({"project": "NOPE99", "name": "x"}, 404), ({"project": key, "name": "a\u0000"}, 400),
                      ({"project": key, "name": "x", "suite": "nope"}, 404), ({"project": key, "name": "x", "suite": "probe-query"}, 409),
                      ({"project": key, "name": "x", "branch": "b" * 256}, 400), ({"project": key, "name": "x", "unknown": 1}, 400)]:
        check(f"start manual {str(body)[:50]}", call(base, "POST", "/test-runs/manual", body)[0], exp)
    check("start manual text/plain", call(base, "POST", "/test-runs/manual", raw=b"{}", ctype="text/plain")[0], 415)
    check("viewer starts a manual run", call(base, "POST", "/test-runs/manual", {"project": key, "name": "x"}, headers=as_viewer)[0], 404)
    st, mrun = call(base, "POST", "/test-runs/manual", {"project": key, "name": "probe sign-off"})
    check("start a manual run", st, 201)
    mid = mrun.get("id", 0) if isinstance(mrun, dict) else 0
    rec = f"/test-runs/{mid}/manual-results"
    for body, exp in [({"testCaseId": hand, "status": "blocked"}, 400), ({"testCaseId": 0, "status": "passed"}, 400),
                      ({"testCaseId": -1, "status": "passed"}, 400), ({"testCaseId": "1", "status": "passed"}, 400),
                      ({"testCaseId": hand, "status": "passed", "note": "n" * 10001}, 400), ({"testCaseId": hand, "status": "passed", "note": "a\u0000"}, 400),
                      ({"testCaseId": hand, "status": "passed", "failedStep": 1}, 400), ({"testCaseId": hand, "status": "failed", "failedStep": 0}, 400),
                      ({"testCaseId": hand, "status": "failed", "failedStep": 2147483648}, 400), ({"testCaseId": hand, "status": "passed", "durationMs": -1}, 400),
                      ({"testCaseId": 9223372036854775807, "status": "passed"}, 409), ({"testCaseId": a, "status": "passed"}, 409),
                      ({"testCaseId": hand, "status": "failed", "note": "ñ" * 10000, "failedStep": 3}, 201)]:
        check(f"record {str(body)[:50]}", call(base, "POST", rec, body)[0], exp)
    for rid, exp in [("0", 400), ("abc", 400), ("9223372036854775807", 404), (str(run_id), 409)]:
        check(f"record into run {rid}", call(base, "POST", f"/test-runs/{rid}/manual-results", {"testCaseId": hand, "status": "passed"})[0], exp)
        check(f"finish run {rid}", call(base, "POST", f"/test-runs/{rid}/finish", {"status": "completed"})[0], exp)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", rec, {"testCaseId": hand, "status": "passed"})[0])) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent records: all kept", codes.count(201), 20)
    st, page = call(base, "GET", f"/test-runs/{mid}/results?pageSize=100")
    attempts = sorted(r["attempt"] for r in page.get("items", [])) if isinstance(page, dict) else []
    check("manual attempts are gapless", attempts == list(range(1, 22)), True)
    for body, exp in [({"status": "interrupted"}, 400), ({"status": "running"}, 400), ({}, 400)]:
        check(f"finish {body}", call(base, "POST", f"/test-runs/{mid}/finish", body)[0], exp)
    check("finish a manual run", call(base, "POST", f"/test-runs/{mid}/finish", {"status": "completed"})[0], 200)
    check("finish it again", call(base, "POST", f"/test-runs/{mid}/finish", {"status": "cancelled"})[0], 409)
    check("record after finishing", call(base, "POST", rec, {"testCaseId": hand, "status": "passed"})[0], 409)

    # Requirements (prototype feature 12): providers, external ids, text and links are validated before any lookup;
    # imports are bounded and idempotent by external id; twenty concurrent native creations get twenty distinct R-n.
    reqs = f"/projects/{key}/requirements"
    for body, exp in [({}, 400), ({"title": " "}, 400), ({"title": "t" * 301}, 400), ({"title": "a\u0000"}, 400),
                      ({"title": "x", "provider": "trello"}, 400), ({"title": "x", "provider": "provenly", "externalId": "R-9"}, 400),
                      ({"title": "x", "provider": "jira"}, 400), ({"title": "x", "provider": "jira", "externalId": "-bad"}, 400),
                      ({"title": "x", "provider": "jira", "externalId": "P" * 101}, 400), ({"title": "x", "url": "ftp://x"}, 400),
                      ({"title": "x", "url": "https://" + "u" * 2000}, 400), ({"title": "x", "providerStatus": "s" * 51}, 400),
                      ({"title": "x", "description": "d" * 10001}, 400), ({"title": "x", "unknown": 1}, 400),
                      ({"title": "ñ" * 300, "provider": "jira", "externalId": "PROBE-1", "url": "https://jira.test/PROBE-1"}, 201),
                      ({"title": "again", "provider": "jira", "externalId": "PROBE-1"}, 409)]:
        check(f"create requirement {str(body)[:50]}", call(base, "POST", reqs, body)[0], exp)
    check("create requirement text/plain", call(base, "POST", reqs, raw=b"{}", ctype="text/plain")[0], 415)
    check("removed member creates a requirement", call(base, "POST", reqs, {"title": "x"}, headers=as_viewer)[0], 404)
    check("removed member lists requirements", call(base, "GET", reqs, headers=as_viewer)[0], 404)
    for body, exp in [({"provider": "provenly", "items": [{"externalId": "R-1", "title": "x"}]}, 400), ({"provider": "jira", "items": []}, 400),
                      ({"provider": "jira", "items": [{"externalId": f"I-{i}", "title": "x"} for i in range(501)]}, 400),
                      ({"provider": "jira", "items": [{"externalId": "I-1", "title": "x"}, {"externalId": "I-1", "title": "y"}]}, 400),
                      ({"provider": "jira", "items": [{"externalId": "I-1"}]}, 400), ({"provider": "jira", "items": [{"externalId": "I-1", "title": "a\u0000"}]}, 400),
                      ({"provider": "jira", "items": [{"externalId": f"I-{i}", "title": "x"} for i in range(500)]}, 200)]:
        check(f"import {str(body)[:50]}", call(base, "POST", f"{reqs}/import", body)[0], exp)
    st, res = call(base, "POST", f"{reqs}/import", {"provider": "jira", "items": [{"externalId": "I-1", "title": "renamed"}, {"externalId": "I-NEW", "title": "n"}]})
    check("re-import updates by external id", f"{st} {res}", "200 {'created': 1, 'updated': 1}")
    rid = call(base, "GET", reqs)[1]["items"][0]["id"]
    for path, method, body, exp in [(f"{reqs}/0", "GET", None, 400), (f"{reqs}/abc", "GET", None, 400), (f"{reqs}/9223372036854775807", "GET", None, 404),
                                    (f"{reqs}/{rid}", "PATCH", {}, 400), (f"{reqs}/{rid}", "PATCH", {"title": ""}, 400), (f"{reqs}/{rid}", "PATCH", {"archived": "yes"}, 400),
                                    (f"{reqs}/9223372036854775807", "PATCH", {"archived": True}, 404), (f"{reqs}/{rid}/test-cases", "PUT", {}, 400),
                                    (f"{reqs}/{rid}/test-cases", "PUT", {"testCaseIds": [0]}, 400), (f"{reqs}/{rid}/test-cases", "PUT", {"testCaseIds": list(range(1, 1002))}, 400),
                                    (f"{reqs}/{rid}/test-cases", "PUT", {"testCaseIds": [9223372036854775807]}, 400),
                                    (f"{reqs}/9223372036854775807/test-cases", "PUT", {"testCaseIds": []}, 404),
                                    (f"{reqs}/{rid}/test-cases", "PUT", {"testCaseIds": [hand, hand]}, 200)]:
        check(f"{method} {path[len(reqs):] or '/'} {str(body)[:30]}", call(base, method, path, body)[0], exp)
    for qs, exp in [(f"testCase={hand}", 200), ("testCase=", 400), ("testCase=0", 400), ("testCase=abc", 400)]:
        check(f"requirements ?{qs}", call(base, "GET", f"{reqs}?{qs}")[0], exp)
    st, page = call(base, "GET", f"{reqs}?testCase={hand}")
    check("requirements covered by a test case", len(page.get("items", [])) if isinstance(page, dict) else st, 1)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", reqs, {"title": "race"})[1].get("externalId"))) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent native requirements: distinct R-n", len(set(codes)), 20)

    # Issues (prototype feature 13, DEC-8): providers, states, text and links are validated before any lookup; imports
    # need a state; verification follows the state and the latest conclusive result; native numbers never collide.
    issues = f"/projects/{key}/issues"
    for body, exp in [({}, 400), ({"title": " "}, 400), ({"title": "x", "state": "done"}, 400), ({"title": "x", "state": ""}, 201),
                      ({"title": "x", "provider": "jira"}, 400), ({"title": "x", "provider": "provenly", "externalId": "I-9"}, 400),
                      ({"title": "x", "url": "javascript:alert(1)"}, 400), ({"title": "a\u0000"}, 400), ({"title": "x", "unknown": 1}, 400),
                      ({"title": "x", "provider": "github", "externalId": "PROBE-1", "state": "closed"}, 201),
                      ({"title": "again", "provider": "github", "externalId": "PROBE-1"}, 409)]:
        check(f"create issue {str(body)[:50]}", call(base, "POST", issues, body)[0], exp)
    check("create issue text/plain", call(base, "POST", issues, raw=b"{}", ctype="text/plain")[0], 415)
    for body, exp in [({"provider": "jira", "items": [{"externalId": "B-1", "title": "x"}]}, 400),
                      ({"provider": "jira", "items": [{"externalId": "B-1", "title": "x", "state": "resolved"}]}, 400),
                      ({"provider": "jira", "items": [{"externalId": f"B-{i}", "title": "x", "state": "open"} for i in range(501)]}, 400),
                      ({"provider": "jira", "items": [{"externalId": "B-1", "title": "x", "state": "open"}]}, 200)]:
        check(f"import issues {str(body)[:50]}", call(base, "POST", f"{issues}/import", body)[0], exp)
    st, page = call(base, "GET", f"{issues}?state=open")
    bug = next((i for i in page.get("items", []) if i["externalId"] == "B-1"), {}) if isinstance(page, dict) else {}
    bid = bug.get("id", 0)
    check("link a test case", call(base, "PUT", f"{issues}/{bid}/test-cases", {"testCaseIds": [hand]})[0], 200)
    st, got = call(base, "GET", f"{issues}/{bid}")
    check("open with a passing manual re-test: not reproducible", got.get("verification", {}).get("status") if isinstance(got, dict) else st, "not_reproducible")
    check("close it", call(base, "PATCH", f"{issues}/{bid}", {"state": "closed"})[0], 200)
    st, got = call(base, "GET", f"{issues}/{bid}")
    check("closed with a passing manual re-test: validated fixed", got.get("verification", {}).get("status") if isinstance(got, dict) else st, "validated_fixed")
    for path, method, body, exp in [(f"{issues}/0", "GET", None, 400), (f"{issues}/9223372036854775807", "GET", None, 404),
                                    (f"{issues}/{bid}", "PATCH", {}, 400), (f"{issues}/{bid}", "PATCH", {"state": "resolved"}, 400),
                                    (f"{issues}/{bid}", "PATCH", {"state": None}, 400), (f"{issues}/{bid}/test-cases", "PUT", {"testCaseIds": [a]}, 400),
                                    (f"{issues}/9223372036854775807/test-cases", "PUT", {"testCaseIds": []}, 404)]:
        check(f"{method} {path[len(issues):] or '/'} {str(body)[:30]}", call(base, method, path, body)[0], exp)
    for qs, exp in [("state=open", 200), ("state=", 400), ("state=OPEN", 400), ("testCase=0", 400), (f"testCase={hand}&state=closed", 200)]:
        check(f"issues ?{qs}", call(base, "GET", f"{issues}?{qs}")[0], exp)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", issues, {"title": "race"})[1].get("externalId"))) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent native issues: distinct I-n", len(set(codes)), 20)

    # Quality (prototype feature 14): bounds are validated before any lookup; counts add up.
    quality = f"/projects/{key}/quality"
    for qs, exp in [("", 200), ("staleDays=1&window=1", 200), ("staleDays=365&window=200", 200), ("staleDays=0", 400),
                    ("staleDays=366", 400), ("staleDays=", 400), ("staleDays=1.5", 400), ("window=201", 400), ("window=-1", 400),
                    ("window=9999999999", 400), ("window=%00", 400)]:
        check(f"quality ?{qs}", call(base, "GET", f"{quality}?{qs}")[0], exp)
    for path, exp in [("/projects/bad/quality", 400), ("/projects/NOPE99/quality", 404)]:
        check(f"GET {path}", call(base, "GET", path)[0], exp)
    st, qual = call(base, "GET", quality)
    tcs = qual.get("testCases", {}) if isinstance(qual, dict) else {}
    check("quality: active = automated + manual", tcs.get("active") == tcs.get("automated", -1) + tcs.get("manual", -1), True)
    check("removed member reads quality", call(base, "GET", quality, headers=as_viewer)[0], 404)

    # Live runs (prototype feature 15): starts and events are validated before any lookup; events are idempotent;
    # twenty concurrent deliveries of one event store it once; the final report completes the run.
    live_id = f"probelive{int(time.time())}"
    start = {"project": key, "provider": "github", "runId": live_id, "runAttempt": 1}
    for body, exp in [({}, 400), ({**start, "runAttempt": 0}, 400), ({**start, "provider": "Bad"}, 400), ({**start, "runId": "a b"}, 400),
                      ({**start, "project": "bad"}, 400), ({**start, "project": "NOPE99"}, 404), ({**start, "suite": "Bad"}, 400),
                      ({**start, "suite": "nope"}, 404), ({**start, "pipeline": "p" * 201}, 400), ({**start, "unknown": 1}, 400),
                      ({"provider": "github", "runId": q.format(1).split("runId=")[1].split("&")[0], "runAttempt": 1}, 409)]:
        check(f"start live {str(body)[:50]}", call(base, "POST", "/test-runs/live", body)[0], exp)
    check("start live text/plain", call(base, "POST", "/test-runs/live", raw=b"{}", ctype="text/plain")[0], 415)
    st, lrun = call(base, "POST", "/test-runs/live", start)
    check("start a live run", st, 201)
    lid = lrun.get("id", 0) if isinstance(lrun, dict) else 0
    check("start it again", call(base, "POST", "/test-runs/live", start)[1].get("id"), lid)
    evs = f"/test-runs/{lid}/events"
    ev = {"eventId": "e1", "sequence": 1, "type": "test.started", "testName": "probe"}
    for body, exp in [({}, 400), ({"events": []}, 400), ({"events": [ev] * 501}, 400), ({"events": [{**ev, "eventId": "a b"}]}, 400),
                      ({"events": [{**ev, "sequence": -1}]}, 400), ({"events": [{**ev, "type": "test.paused"}]}, 400),
                      ({"events": [{**ev, "type": "test.finished"}]}, 400), ({"events": [{**ev, "status": "passed"}]}, 400),
                      ({"events": [{**ev, "testName": "n" * 1001}]}, 400), ({"events": [{**ev, "testName": "a\u0000"}]}, 400),
                      ({"events": [{**ev, "occurredAt": "yesterday"}]}, 400), ({"events": [{**ev, "unknown": 1}]}, 400),
                      ({"events": [{**ev, "attempt": 101}]}, 400), ({"events": [{**ev, "attempt": -1}]}, 400), ({"events": [{**ev, "attempt": "2"}]}, 400),
                      ({"events": [{**ev, "testCase": "TC-99999999"}]}, 200)]:
        check(f"events {str(body)[:50]}", call(base, "POST", evs, body)[0], exp)
    check("events text/plain", call(base, "POST", evs, raw=b"{}", ctype="text/plain")[0], 415)
    for rid, exp in [("0", 400), ("abc", 400), ("9223372036854775807", 404), (str(run_id), 409)]:
        check(f"events into run {rid}", call(base, "POST", f"/test-runs/{rid}/events", {"events": [{**ev, "eventId": "z"}]})[0], exp)
        check(f"live of run {rid}", call(base, "GET", f"/test-runs/{rid}/live")[0], exp)
    codes = []
    threads = [threading.Thread(target=lambda: codes.append(call(base, "POST", evs, {"events": [{**ev, "eventId": "race"}]})[1].get("accepted"))) for _ in range(20)]
    [t.start() for t in threads]
    [t.join() for t in threads]
    check("20 concurrent deliveries of one event: stored once", codes.count(1), 1)
    st, lv = call(base, "GET", f"/test-runs/{lid}/live")
    check("live state while running", lv.get("reconciliation") if isinstance(lv, dict) else st, "pending")
    st, body = call(base, "POST", f"/ingestion/junit?project={key}&provider=github&runId={live_id}&runAttempt=1", raw=xml, ctype="application/xml")
    check("the report completes the live run", f"{st} {body.get('testRun', {}).get('executionStatus') if isinstance(body, dict) else ''}", "201 completed")
    st, lv = call(base, "GET", f"/test-runs/{lid}/live")
    check("reconciled after the report", lv.get("reconciliation") if isinstance(lv, dict) else st, "mismatch")
    check("events after the report", call(base, "POST", evs, {"events": [{**ev, "eventId": "late"}]})[0], 409)

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
