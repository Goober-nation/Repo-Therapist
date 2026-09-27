#!/usr/bin/env python3
"""Creates the full grading mock repos (backlog phase 8) under timoxa102928 on SourceCraft.

Every repo gets: backdated multi-author git history with TODO/FIXME markers of chosen ages,
a docs set of one of five quality levels, seeded security issues for AppSec's automatic
per-push scan, milestones (deadlines set in the past), issues (some closed), and manually
triggered CI runs with chosen outcomes/durations (workflow inputs, so no extra commits).

Usage: SOURCECRAFT_PAT=... generate.py [slug ...]   (no args = all repos)
Idempotent: a repo that already has content is skipped. Writes docs/mock-repos.json.
Issues can't be backdated (server time) — see docs/mock-repos.json "known_limits".
"""
import json
import os
import random
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

ORG = os.environ.get("MOCK_ORG", "timoxa102928")
API = "https://api.sourcecraft.tech"
PAT = os.environ["SOURCECRAFT_PAT"]
ROOT = Path(__file__).resolve().parents[3]
NOW = datetime.now(timezone.utc).replace(microsecond=0)

AUTHORS = [("Anna Petrova", "anna"), ("Boris Ivanov", "boris"), ("Chen Wei", "chen"),
           ("Dana Smirnova", "dana"), ("Egor Kuznetsov", "egor"), ("Fatima Aliyeva", "fatima"),
           ("Gleb Orlov", "gleb")]

# Security seeds. AppSec scans every push; findings come from real engines (Trivy/grype on
# go.mod, Opengrep on code). Old golang.org/x/crypto alone gave 55 open groups on the probe.
SEC_DEPS = {
    "clean": [],
    "code": [],
    "dep": ["golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9"],
    "multi": ["golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9",
              "golang.org/x/net v0.0.0-20200226121028-0de0cce0169b",
              "gopkg.in/yaml.v2 v2.2.2"],
}

# Level knobs per repo. commits: (count in last 180d, count 180-365d); last: days since last
# commit; authors: how many, weights skew top-contributor share; markers: list of (age_days, file);
# milestones: deadlines in days ago; issues: (total, closed); ci: [(outcome, sleep_s)] oldest first.
def markers(n, age_lo, age_hi, files):
    rnd = random.Random(n * 1000 + age_lo)
    return [(rnd.randint(age_lo, age_hi), files[i % len(files)]) for i in range(n)]

SPREAD = ["api.go", "store.go", "util.go", "handler.go", "config.go"]
REPOS = [
    dict(slug="health-mock-exemplary", title="Exemplary project", levels="all high",
         commits=(130, 60), last=1, authors=6, weights=[3, 3, 2, 2, 2, 1], loc=3000,
         markers=markers(2, 20, 60, SPREAD), docs=4, sec="clean",
         milestones=[20, 110, 200, 290], issues=(12, 11),
         ci=[(0, 60)] * 8),
    dict(slug="health-mock-strong", title="Strong project", levels="high, medium security/docs",
         commits=(100, 50), last=2, authors=5, weights=[4, 3, 2, 2, 1], loc=2500,
         markers=markers(4, 30, 150, SPREAD), docs=3, sec="code",
         milestones=[35, 150, 260], issues=(10, 8),
         ci=[(0, 90)] * 7 + [(1, 90)]),
    dict(slug="health-mock-docs-first", title="Documentation-first project", levels="great docs, weak CI",
         commits=(40, 30), last=8, authors=3, weights=[3, 2, 1], loc=1500,
         markers=markers(6, 100, 250, SPREAD[:3]), docs=4, sec="clean",
         milestones=[80, 250], issues=(8, 5),
         ci=[(1, 120), (0, 120), (1, 120), (1, 120), (0, 120), (1, 120)]),
    dict(slug="health-mock-average", title="Average project", levels="medium everywhere, flaky CI",
         commits=(35, 25), last=10, authors=3, weights=[4, 2, 1], loc=1800,
         markers=markers(8, 120, 300, SPREAD[:3]), docs=2, sec="code",
         milestones=[90, 300], issues=(10, 6),
         ci=[(0, 180), (1, 180), (0, 180), (0, 180), (1, 180), (0, 180), (1, 180), (0, 180)]),
    dict(slug="health-mock-average-b", title="Average project B", levels="medium, weak code health and security",
         commits=(30, 20), last=15, authors=2, weights=[3, 1], loc=1200,
         markers=markers(12, 200, 340, SPREAD[:2]), docs=1, sec="dep",
         milestones=[120], issues=(9, 4),
         ci=[(0, 240), (0, 240), (1, 240), (0, 240), (0, 240), (1, 240)]),
    dict(slug="health-mock-busy-messy", title="Busy but messy project", levels="high activity, low code health/docs",
         commits=(150, 40), last=1, authors=7, weights=[2, 2, 2, 2, 1, 1, 1], loc=2000,
         markers=markers(25, 150, 350, ["legacy.go"] * 3 + SPREAD), docs=1, sec="dep",
         milestones=[50, 200], issues=(14, 7),
         ci=[(0, 300), (1, 300), (0, 300), (1, 300), (0, 300), (0, 300)]),
    dict(slug="health-mock-abandoned", title="Abandoned project", levels="dormant 200+ days",
         commits=(0, 45), last=210, authors=2, weights=[5, 1], loc=1500,
         markers=markers(7, 220, 330, SPREAD[:3]), docs=2, sec="clean",
         milestones=[330], issues=(8, 1),
         ci=[(1, 600), (1, 600), (0, 600), (1, 600)]),
    dict(slug="health-mock-tiny-lib", title="Tiny single-author library", levels="solo, clean, well documented",
         commits=(8, 10), last=30, authors=1, weights=[1], loc=600,
         markers=[], docs=3, sec="clean",
         milestones=[], issues=(3, 2),
         ci=[(0, 60), (0, 60), (1, 60), (0, 60), (0, 60)]),
    dict(slug="health-mock-ci-red", title="Healthy code, broken CI", levels="high except CI",
         commits=(110, 40), last=1, authors=5, weights=[3, 2, 2, 1, 1], loc=2500,
         markers=markers(3, 30, 100, SPREAD), docs=3, sec="code",
         milestones=[25, 140, 250], issues=(11, 10),
         ci=[(0, 600), (1, 600), (1, 600), (1, 600), (1, 600), (1, 600)]),
    dict(slug="health-mock-disaster", title="Disaster project", levels="low everywhere",
         commits=(3, 12), last=160, authors=1, weights=[1], loc=900,
         markers=markers(30, 280, 360, ["main.go", "main.go", "main.go", "util.go"]), docs=0, sec="multi",
         milestones=[], issues=(12, 1),
         ci=[(1, 720)] * 6),
]


def api(method, path, body=None):
    req = urllib.request.Request(API + path, method=method,
                                 data=json.dumps(body).encode() if body is not None else None,
                                 headers={"Authorization": "Bearer " + PAT, "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")


def log(slug, msg):
    print(f"[{datetime.now():%H:%M:%S}] {slug}: {msg}", flush=True)


# ---------- repo content ----------

def go_mod(spec):
    reqs = "".join(f"\t{d}\n" for d in SEC_DEPS[spec["sec"]])
    body = f"module sourcecraft.dev/{ORG}/{spec['slug']}\n\ngo 1.21\n"
    return body + (f"\nrequire (\n{reqs})\n" if reqs else "")


def security_code(spec):
    if spec["sec"] in ("clean",):
        return None
    return '''package app

import (
	"crypto/md5"
	"fmt"
	"os/exec"
)

// RunHook executes a user-configured shell hook.
func RunHook(cmd string) error {
	return exec.Command("sh", "-c", cmd).Run()
}

// Checksum returns a quick fingerprint of data.
func Checksum(data []byte) string {
	return fmt.Sprintf("%x", md5.Sum(data))
}
'''


DOC_README = {
    1: "# {name}\n\nSmall Go service.\n",
    2: """# {name}

{title}: a Go service that stores and serves records over HTTP.

## Build

    go build ./...

## Run

    go run . --port 8080
""",
    3: """# {name}

{title}: a Go service that stores and serves records over HTTP. It keeps an in-memory index,
persists snapshots to disk and exposes a small JSON API.

## Requirements

- Go 1.21+

## Build and test

    go build ./...
    go test ./...

## Run locally

    go run . --port 8080
    curl http://localhost:8080/health   # -> {{"status":"ok"}}

## Configuration

| Flag | Default | Meaning |
|---|---|---|
| `--port` | 8080 | HTTP port |
| `--data` | ./data | snapshot directory |

## Project structure

- `main.go` - entry point and flag parsing
- `app/` - request handlers, storage and helpers

## License

MIT, see LICENSE.
""",
}


def docs_files(spec):
    lvl, name = spec["docs"], spec["slug"]
    files = {}
    if lvl == 0:
        return files
    if lvl >= 4:
        files["README.md"] = DOC_README[3].format(name=name, title=spec["title"]) + """
## Contributing

See CONTRIBUTING.md. Architecture notes are in docs/architecture.md, API reference in docs/api.md.
"""
    else:
        files["README.md"] = DOC_README[lvl].format(name=name, title=spec["title"])
    if lvl >= 2:
        files["LICENSE"] = "MIT License\n\nCopyright (c) 2025 " + name + "\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software, to deal in the Software without restriction.\n"
    if lvl >= 3:
        files["CONTRIBUTING.md"] = "# Contributing\n\n1. Open an issue describing the change.\n2. Fork, create a branch, run `go test ./...`.\n3. Open a pull request; one approval is required.\n"
    if lvl >= 4:
        files["CODEOWNERS"] = "* @" + ORG + "\n"
        files["CHANGELOG.md"] = "# Changelog\n\n## 1.2.0\n- Snapshot compression\n\n## 1.1.0\n- Health endpoint\n"
        files["docs/architecture.md"] = "# Architecture\n\nRequests hit `app/handler.go`, which validates input and calls `app/store.go`.\nThe store keeps an in-memory index and writes snapshots every minute.\n"
        files["docs/api.md"] = "# API\n\n| Method | Path | Description |\n|---|---|---|\n| GET | /health | liveness |\n| GET | /records/{id} | fetch a record |\n| POST | /records | create a record |\n"
    return files


CI_YAML = """workflows:
  build:
    inputs:
      outcome:
        type: string
        default: "0"
      sleep_s:
        type: string
        default: "1"
    tasks:
      - name: test
        cubes:
          - name: run
            script:
              - sleep ${{ inputs.sleep_s }}
              - exit ${{ inputs.outcome }}
"""


def func_block(i, n_lines):
    body = "".join(f"\tv{j} := x + {j}\n\tx = v{j} * 2\n" for j in range(max(1, (n_lines - 4) // 2)))
    return f"\n// step{i} is generated filler logic.\nfunc step{i}(x int) int {{\n{body}\treturn x\n}}\n"


def git(cwd, *args, env=None):
    subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True,
                   env={**os.environ, **(env or {})})


def commit(cwd, author, when, msg):
    name, handle = author
    ts = when.strftime("%Y-%m-%dT%H:%M:%S+0000")
    git(cwd, "add", "-A")
    git(cwd, "commit", "-q", "--allow-empty", "-m", msg, env={
        "GIT_AUTHOR_NAME": name, "GIT_AUTHOR_EMAIL": f"{handle}@example.invalid",
        "GIT_COMMITTER_NAME": name, "GIT_COMMITTER_EMAIL": f"{handle}@example.invalid",
        "GIT_AUTHOR_DATE": ts, "GIT_COMMITTER_DATE": ts})


def build_history(spec, cwd):
    rnd = random.Random(spec["slug"])
    authors = AUTHORS[:spec["authors"]]
    recent, older = spec["commits"]
    last = spec["last"]
    # commit timestamps: `older` spread over 180-365d ago, `recent` over 180d..last days ago
    times = [NOW - timedelta(days=rnd.uniform(max(180, last), 364)) for _ in range(older)]
    if recent:
        times += [NOW - timedelta(days=rnd.uniform(last, 179)) for _ in range(recent - 1)]
        times.append(NOW - timedelta(days=last, hours=2))
    else:
        times.append(NOW - timedelta(days=last, hours=2))
    times.sort()
    events = [(t, "code", None) for t in times]
    for age, f in spec["markers"]:
        events.append((NOW - timedelta(days=age, hours=rnd.randint(0, 20)), "marker", f))
    events.sort(key=lambda e: e[0])
    # first event: skeleton
    first = events[0][0] - timedelta(hours=1)
    app = cwd / "app"
    app.mkdir()
    (cwd / "go.mod").write_text(go_mod(spec))
    (cwd / "main.go").write_text('package main\n\nimport "fmt"\n\nfunc main() {\n\tfmt.Println("started")\n}\n')
    for f in set(SPREAD + [m[1] for m in spec["markers"] if m[1] != "main.go"]):
        (app / f).write_text("package app\n")
    for path, text in docs_files(spec).items():
        p = cwd / path
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text)
    sec = security_code(spec)
    if sec:
        (app / "hooks.go").write_text(sec)
    (cwd / ".sourcecraft").mkdir()
    (cwd / ".sourcecraft" / "ci.yaml").write_text(CI_YAML)
    commit(cwd, authors[0], first, "Initial project skeleton")
    per_commit = max(6, spec["loc"] // max(1, len(times)))
    code_files = [f for f in SPREAD]
    n = 0
    for when, kind, f in events:
        author = rnd.choices(authors, weights=spec["weights"])[0]
        if kind == "code":
            n += 1
            target = app / code_files[n % len(code_files)]
            with target.open("a") as fh:
                fh.write(func_block(n, per_commit))
            commit(cwd, author, when, f"Add step{n}")
        else:
            target = cwd / "main.go" if f == "main.go" else app / f
            tag = rnd.choice(["TODO", "FIXME"])
            with target.open("a") as fh:
                fh.write(f"\n// {tag}: revisit this part, edge cases are not handled ({n}-{rnd.randint(100, 999)})\n")
            commit(cwd, author, when, f"Note follow-up in {f}")


# ---------- platform side ----------

def run_ci(spec, repo_path):
    for i, (outcome, sleep_s) in enumerate(spec["ci"], 1):
        code, run = api("POST", f"{repo_path}/cicd/runs", {"workflows": [{"name": "build", "values": [
            {"name": "outcome", "value": str(outcome)}, {"name": "sleep_s", "value": str(sleep_s)}]}]})
        if code >= 300:
            log(spec["slug"], f"CI trigger {i} failed: {code} {run}")
            return
        slug = run.get("slug")
        deadline = time.time() + sleep_s + 900
        while time.time() < deadline:
            time.sleep(15)
            _, runs = api("GET", f"{repo_path}/cicd/runs")
            st = next((r["status"] for r in runs.get("runs", []) if r["slug"] == slug), None)
            if st in ("success", "failed", "failure", "canceled", "cancelled", "error"):
                break
        log(spec["slug"], f"CI run {i}/{len(spec['ci'])}: expected {'fail' if outcome else 'pass'}, got {st}")


def build(spec):
    slug = spec["slug"]
    repo_path = f"/repos/{ORG}/{slug}"
    code, repo = api("GET", repo_path)
    if code == 200 and not repo.get("is_empty", True):
        log(slug, "exists with content, skipping")
        return repo
    if code == 404:
        code, repo = api("POST", f"/orgs/{ORG}/repos", {"slug": slug, "visibility": "public",
                         "description": f"Grading mock repo ({spec['levels']}), generated by scripts/mock-repos/full"})
        if code >= 300:
            raise RuntimeError(f"{slug}: create failed {code} {repo}")
        log(slug, "created")
    with tempfile.TemporaryDirectory() as tmp:
        cwd = Path(tmp)
        git(cwd, "init", "-q", "-b", "main")
        build_history(spec, cwd)
        remote = f"https://oauth:{PAT}@git.sourcecraft.dev/{ORG}/{slug}.git"
        r = subprocess.run(["git", "push", "-q", remote, "main"], cwd=cwd, capture_output=True, text=True)
        if r.returncode:
            raise RuntimeError(f"{slug}: push failed: {r.stderr.replace(PAT, '***')}")
    log(slug, "history pushed")
    for i, days_ago in enumerate(spec["milestones"], 1):
        dl = (NOW - timedelta(days=days_ago)).strftime("%Y-%m-%dT00:00:00Z")
        st = (NOW - timedelta(days=days_ago + 30)).strftime("%Y-%m-%dT00:00:00Z")
        _, m = api("POST", f"{repo_path}/milestones", {"name": f"v1.{i}", "slug": f"v1-{i}", "start_date": st, "deadline": dl})
        api("PATCH", f"{repo_path}/milestones/v1-{i}", {"status": "closed"})
    total, closed = spec["issues"]
    rnd = random.Random(slug + "issues")
    topics = ["Crash on empty input", "Add pagination", "Improve error messages", "Flaky test in store",
              "Document config flags", "Support TLS", "Memory grows over time", "Add metrics endpoint",
              "Rename confusing flag", "Upgrade Go version", "Slow startup", "Validate IDs", "Add retries", "Timezone bug"]
    slugs = []
    for i in range(total):
        c, iss = api("POST", f"{repo_path}/issues", {"title": topics[i % len(topics)], "description": "Generated mock issue."})
        if c < 300:
            slugs.append(iss["slug"])
    for s in rnd.sample(slugs, min(closed, len(slugs))):
        api("PATCH", f"{repo_path}/issues/{s}", {"status_slug": "closed"})
    log(slug, f"{len(spec['milestones'])} milestones, {len(slugs)} issues ({closed} closed)")
    run_ci(spec, repo_path)
    _, repo = api("GET", repo_path)
    return repo


def main():
    wanted = set(sys.argv[1:])
    specs = [s for s in REPOS if not wanted or s["slug"] in wanted]
    results, threads = {}, []

    def worker(s):
        try:
            results[s["slug"]] = build(s)
        except Exception as e:  # keep other repos going
            log(s["slug"], f"FAILED: {e}")

    for s in specs:
        t = threading.Thread(target=worker, args=(s,))
        t.start()
        threads.append(t)
    for t in threads:
        t.join()

    out = ROOT / "docs" / "mock-repos.json"
    doc = json.loads(out.read_text()) if out.exists() else {}
    doc.update({
        "purpose": "Full grading fixtures on real SourceCraft (backlog phase 8): every category has data, levels vary. Created by scripts/mock-repos/full/generate.py.",
        "org": ORG,
        "known_limits": ["Issue created/closed times are server time (not backdatable): abandoned_share and median_closure_days sit at their healthy extreme in every repo.",
                         "CI runs all happen at generation time.",
                         "Security findings come from AppSec's automatic per-push scan (Trivy/grype/Opengrep); counts are what the engines report."],
    })
    repos = {r["repo"]: r for r in doc.get("repos", [])}
    for s in REPOS:
        if s["slug"] in results:
            repos[s["slug"]] = {"repo": s["slug"], "sourcecraft_uuid": results[s["slug"]].get("id"),
                                "title": s["title"], "intended_levels": s["levels"], "docs_level": s["docs"],
                                "security_seed": s["sec"], "ci_runs_oldest_first": " ".join("F" if o else "S" for o, _ in s["ci"]),
                                "issues_total_closed": list(s["issues"]), "milestone_deadlines_days_ago": s["milestones"],
                                "commits_recent_older": list(s["commits"]), "days_since_last_commit": s["last"],
                                "markers": len(s["markers"])}
    doc["repos"] = [repos[s["slug"]] for s in REPOS if s["slug"] in repos]
    out.write_text(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")
    print(f"wrote {out}")


if __name__ == "__main__":
    main()
