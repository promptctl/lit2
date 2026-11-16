# A path-addressed CLI: examples

Worked examples for the path grammar of `path-addressed-cli.md`, under the model
`architecture.md` fixes, which is the authority on every command here. Nothing is
implemented; the output shown is what the design says a terminal will print.

The examples are grounded in 4,319 Claude Code sessions that ran `lit`, 24,223
invocations in all. Where the old command was short, it sits above the new one as
`was:`. The most common session in the transcripts is a plan review, then a worker
taking a ticket from `next` to `done`, so those come first. Shell quoting is as the
design states: a selector, glob, list, or object is single-quoted.

## Reviewing a plan

### Read what a plan reviewer is told before judging

    was: lit quickstart review
    $ lit get atb
    …
    guidance (plan): Before any code, write the plan: what changes, in which files, and how it is proven.
    A reviewer other than the author judges it against the laws and records one review of the revision they read.

### Read the ticket under review

    was: lit show mock-mock-flu.atb
    $ lit get atb
    $ = mock-mock-flu.atb
    $/title          The mock's token refresh retries on a clock the test controls
    $/status         plan
    $/parent         mock-mock-flu
    $/lane           atb
    $/claim          claude_7f2e… (elsewhere)
    $/body           The refresh path in src/oauth.ts sleeps on the wall clock …
    $/plan           revision 7
    $/plan/verdict   (none)
    $/comments/43s   [claude_7f2e…] revision 7 moves the clock behind the injected Clock port …

    guidance (plan): Before any code, write the plan: …

### Read only the plan text

    was: lit show promptctl-brain-vu2t.abp --field plan > /tmp/abp_break/plan_v4.md
    $ lit get abp/plan > /tmp/abp_break/plan_v4.md

### Get the revision to name in the review, and see whether a verdict already exists

    was: lit show promptctl-brain-q9s8.45d | grep -o 'plan (v[0-9]*, digest [0-9a-f]*' | tail -1
    $ lit get 45d/plan 45d/plan/verdict
    $ = promptctl-brain-q9s8.45d
    $/plan          revision 7
    $/plan/verdict  (none)

### Check whether this plan revision was already reviewed

    was: lit show promptctl-brain-vu2t.abp | grep -c "Plan review (digest f2623ef71b5d)"
    $ lit get abp/plan/reviews
    $ = promptctl-brain-vu2t.abp
    $/plan/reviews/r4x   fail  of:3  how:"/plan-review"  result:"nothing-unseen: …"
    $/plan/reviews/9pe   fail  of:4  how:"/plan-review"  result:"single-enforcer, one-source-of-truth: …"

### Read the epic's sibling tickets to see what the plan must fit beside

    was: for t in 0m9 faz 03v 0go w0o; do lit show mock-mock-flu.$t | sed -n '1,/^Epic:/p' | grep -v '^sync:'; done
    $ lit get 'flu.*/title' 'flu.*/status'
    flu.0m9/title    The mock serves streamed responses in recorded chunk order
    flu.faz/title    Credential login writes one file, read by every client
    flu.03v/title    The mock's request log names the session that sent each call
    flu.0go/title    A recorded fixture refuses a request it has no match for
    flu.w0o/title    The CLI flag set is parsed once into a typed config
    flu.0m9/status   done
    flu.faz/status   impl
    flu.03v/status   backlog
    flu.0go/status   backlog
    flu.w0o/status   plan

### Search for earlier tickets on the same mechanism before judging novelty

    was: lit ls --search powerd --status open,in_progress,closed
    $ lit get '(text~"powerd")/status'
    orange-devices-bue.6on/status   plan
    orange-devices-bue.hqi/status   done
    orange-power-k3c.2wd/status     obsolete

### Write the reasons for a fail before recording it

    was: lit comment add mock-mock-flu.atb --body-file - <<'REASONS'
    $ lit add atb/comments - <<'REASONS'
    Plan review (digest b8fa35fac99f): fail.
    1. no-ambient-temporal-coupling: step 3 retries on setTimeout, outside the injected Clock.
    2. nothing-unseen: the refresh failure is caught in step 4 and logged nowhere.
    REASONS
    + atb/comments/7kq

### Record the fail review, citing the laws

    was: lit verdict mock-mock-flu.atb fail --plan b8fa35fac99f… --law no-ambient-temporal-coupling --law nothing-unseen
    $ lit add atb/plan/reviews '{of:7 how:"/plan-review" result:"no-ambient-temporal-coupling, nothing-unseen: …" verdict:fail}'
    + atb/plan/reviews/r4x
    ⇒ atb/plan/verdict = fail

### Record a pass review

    was: lit verdict rich-joiners-er38.a1h pass --plan cea7cfc9d682… --law dataflow-not-control-flow --law types-are-the-program
    $ lit add a1h/plan/reviews '{of:4 how:"/plan-review" result:"dataflow-not-control-flow, types-are-the-program: …" verdict:pass}'
    + a1h/plan/reviews/b97
    ⇒ a1h/plan/verdict = pass

### A review that names no revision is refused

    $ lit add atb/plan/reviews '{verdict:fail}'
    error (reason=requires): a review names the revision it judged: of; nothing written

### The plan changed between reading it and recording the review

The reviewer read revision 7. The author appended revision 8 while the review ran.

    $ lit add atb/plan/reviews '{of:7 how:"/plan-review" result:"single-enforcer: …" verdict:pass}'
    + atb/plan/reviews/c2m
    ⇒ atb/plan/verdict = (none): latest revision is 8, last reviewed 7

### Confirm the review landed, not only the comment

152 of 296 reviewers in one slice wrote a review comment and recorded no verdict.

    $ lit get atb/plan/verdict
    fail

### The plan's author passes their own plan

    $ lit add s4l/plan/reviews '{of:1 how:"/plan-review" result:"one-source-of-truth: …" verdict:pass}'
    + s4l/plan/reviews/q1z
    ⇒ s4l/plan/verdict = pass

## Plan revisions and reviews

### Has the current plan been reviewed yet?

Before review, then after a reviewer passes revision 43.

    was: lit show 8ro6 | grep -nE '^plan \(v|reviews of plan'
    $ lit get 8ro6/plan/verdict
    (none)
    $ lit get 8ro6/plan/verdict
    pass  815  claude_9f1e…  of:43

### List every revision of a plan

    $ lit get 8ro6/plan/revisions
    8ro6/plan/revisions
      1   claude_2eb9…  2026-10-21T03:12Z
      2   claude_2eb9…  2026-10-21T04:40Z
      …
      21  claude_2eb9…  2026-10-24T18:40Z

### Read plan revision 3

    was: lit history 8ro6 --version plan@3
    $ lit get 8ro6/plan/revisions/3
    ## Plan
    1. The watcher derives its target through one builder …

### Diff two plan revisions

    $ diff <(lit get 8ro6/plan/revisions/20) <(lit get 8ro6/plan/revisions/21)
    14c14
    < 3. The cache keys on three positional strings.
    ---
    > 3. The cache keys on a render origin.

### Record a verdict directly instead of adding a review

    $ lit set atb/plan/verdict pass
    error (reason=derived): atb/plan/verdict is derived (the verdict of the last review naming the latest revision); add one: lit add atb/plan/reviews '{of:8 how:"…" result:"…" verdict:pass}'; nothing written

### Find plans waiting on a reviewer

    $ lit get '(status=plan plan/verdict="")/title'
    8ro6/title   The render cache takes a render origin, not three positional strings
    nlf/title    A level's form is decided once, at parse

### Revise a plan after it passed

    was: lit update 8ro6 --plan-file /tmp/8ro6-ticket/plan.md
    $ lit add 8ro6/plan/revisions - < /tmp/8ro6-ticket/plan.md
    + 8ro6/plan/revisions/43
    ⇒ 8ro6/plan/verdict = (none)   was pass on revision 42

### Revise a plan while the ticket is already in impl

    $ lit add 8ro6/plan/revisions - < /tmp/8ro6-ticket/plan.md
    + 8ro6/plan/revisions/44
    ⇒ 8ro6/plan/verdict = (none)   was pass on revision 43

### My review names a revision that is no longer the latest

I read revision 21; the author appended revision 22 while I was reviewing.

    was: lit verdict 8ro6 pass --plan f76132367de4b85c… --law one-source-of-truth   (refused: stale digest)
    $ lit add 8ro6/plan/reviews '{of:21 how:"/plan-review" result:"one-source-of-truth: …" verdict:pass}'
    + 8ro6/plan/reviews/81f
    ⇒ 8ro6/plan/verdict = (none): latest revision is 22, last reviewed 21

## Working a ticket, start to done

### Find the next ticket

    was: lit next
    $ lit get /next
    /next   s4l   /lanes/history 1/3   top-ranked unclaimed lane

### See the whole lane before starting it

    $ lit get /lanes/history
    /lanes/history  rank 1
      s4l   backlog
      7ka   backlog
      m3d   backlog

### Start it

    was: lit start orange-history-4y3.s4l
    $ lit set s4l/status plan
    s4l/status  backlog → plan
    ⇒ s4l/claim = this checkout
    ⇒ /lanes/history  held by this checkout

### Start a ticket that is its own lane

    was: lit start brandon-menus-sjhy.ju8
    $ lit set ju8/status plan
    ju8/status  backlog → plan
    ⇒ ju8/claim = this checkout

### Starting a ticket behind another in its lane is refused

    $ lit set fw9/status plan
    error (reason=gate): fw9/status backlog → plan refused by backlog/exit; nothing written
      block  !blockers   fw9/blockers = [b64]  (ahead of it in /lanes/datetime)

### Write the plan from stdin

    was: lit update orange-history-4y3.s4l --plan-file /tmp/s4l-plan.md
    $ lit add s4l/plan/revisions - <<'EOF'
    1. Move the history query behind HistoryStore (src/history/store.ts).
    2. …
    Proof: test/history.test.ts covers an empty store and a 10k-row store.
    EOF
    + s4l/plan/revisions/1

### Revise the plan after a fail

    was: lit update brandon-unfiled-3248.8ro6 --plan-file /tmp/8ro6-ticket/plan.md && lit show brandon-unfiled-3248.8ro6 | grep -nE '^plan \(v|reviews of plan'
    $ lit add 8ro6/plan/revisions - < /tmp/8ro6-ticket/plan.md
    + 8ro6/plan/revisions/22
    ⇒ 8ro6/plan/verdict  fail → (none)

### Read the previous plan revision to diff against

    was: lit history brandon-unfiled-3248.8ro6 --version plan@21
    $ lit get 8ro6/plan/revisions/21 > /tmp/8ro6-ticket/plan.r21.md

### Move to impl once the plan has passed

    was: lit phase orange-history-4y3.s4l --to impl
    $ lit set s4l/status impl
    s4l/status  plan → impl

### Moving to impl with no verdict is refused

    $ lit set s4l/status impl
    error (reason=gate): s4l/status plan → impl refused by impl/entry; nothing written
      block  plan/verdict=pass   s4l/plan/verdict = (none)

### Moving to impl on a failed plan is refused

    $ lit set atb/status impl
    error (reason=gate): atb/status plan → impl refused by impl/entry; nothing written
      block  plan/verdict=pass   atb/plan/verdict = fail (of:7)

### Trying to finish a ticket whose plan was never reviewed

`done` is named by `merge/next` alone, so it is entered only from `merge`.

    was: lit done orange-devices-bue.355
    $ lit set 355/status done
    error (reason=not_in_next): 355/status plan → done; plan/next = [impl]; nothing written

### Moving a backlog ticket straight to merge is refused

    was: lit phase promptctl-schema-reversal-u0if.0ca --to merge
    $ lit set 0ca/status merge
    error (reason=not_in_next): 0ca/status backlog → merge; backlog/next = [plan]; nothing written

### Hand the branch to review

    was: lit phase orange-history-4y3.s4l --to review
    $ lit set s4l/status review
    s4l/status  impl → review

### Move to merge after the last review pass

    was: lit phase orange-history-4y3.s4l --to merge
    $ lit set s4l/status merge
    s4l/status  review → merge

### Finish it; the lane hands this checkout the next ticket

    was: lit done template-unfiled-792a.dee
    $ lit set dee/status done
    dee/status  merge → done
    ⇒ dee/claim  this checkout → (none)
    ⇒ /lanes/datetime  head is now b64
    ⇒ b64/claim  (none) → this checkout

### Finish it with the merge on record

    was: lit done houdini-laws-etg.79h --reason "Merged in #129 (07e20a1). Follow-up: houdini-laws-etg.tva."
    $ lit set 79h/status done 79h/reason "Merged in #129 (07e20a1). Follow-up: houdini-laws-etg.tva."
    79h/status  merge → done
    79h/reason  "" → "Merged in #129 (07e20a1). Follow-up: houdini-laws-etg.tva."
    ⇒ 79h/claim  this checkout → (none)
    ⇒ /lanes/scene-load  head is now lbc
    ⇒ lbc/claim  (none) → this checkout

### Finish the last ticket in a lane

    $ lit set fw9/status done
    fw9/status  merge → done
    ⇒ fw9/claim  this checkout → (none)
    ⇒ /lanes/datetime  finished; this checkout holds no lane

### See what is served after the lane is finished

    $ lit get /next
    /next   bryp   /lanes/sync-p0 1/1   top-ranked unclaimed lane

### Starting a ticket whose lane another checkout holds

Nothing coordinates a write, so the start lands; the lane now has two holders, and
the latest in total order is its `claim`.

    was: lit start links-plan-review-o3mh.avl
    $ lit set avl/status plan
    avl/status  backlog → plan
    ⇒ avl/claim = this checkout
    ⇒ /lanes/plan-review  held by this checkout; contested: stream ufijgjuy (elsewhere) · 5m

### Take the lane over cleanly: release the hold, then start

    was: lit start links-plan-review-o3mh.avl --take
    $ lit rm /lanes/plan-review/claim
    - /lanes/plan-review/claim  stream ufijgjuy (elsewhere)
    $ lit set avl/status plan
    avl/status  backlog → plan
    ⇒ avl/claim = this checkout
    ⇒ /lanes/plan-review  held by this checkout

### Take over a ticket another checkout left in impl

A hold is established by a start or a finish. Moving `impl → review` is neither, so
the lane stays unheld until this checkout finishes a member.

    was: lit start candybar-daemon-health-ajx0.co4 --take
    $ lit rm /lanes/daemon-health/claim
    - /lanes/daemon-health/claim  stream 8c1d02aa (elsewhere)
    $ lit set co4/status review
    co4/status  impl → review

### Check who holds a lane before deciding to take it

    was: lit backlog 2>&1 | grep -n -B2 -A3 -i 'bryp\|claim'
    $ lit get /lanes/plan-review
    /lanes/plan-review  rank 3
      avl   backlog   stream ufijgjuy (elsewhere), claimed 5m ago
      bjm   backlog

### Publish ticket writes at the end of the session

Every write already schedules a detached sync; running it by hand waits for it.

    was: lit sync push
    $ lit admin sync
    sealed 1 segment, published; fetched 2 writers, verified

## Claims

### Take over a lane whose holder is dead

    was: lit start 9u9 --take
    $ lit add 9u9/comments "Took the px1f lane from voice-di1: no process has that worktree as cwd, no tmux pane, tree clean."
    + 9u9/comments/b7a0
    $ lit rm /lanes/px1f/claim
    - /lanes/px1f/claim   stream voice-di1 (elsewhere) · 3h
    $ lit set 9u9/status review
    9u9/status  impl → review

### A claim that has expired

An expired claim is no claim; it prints as none, with why.

    $ lit get bci/claim
    (none)   claude_214f… (elsewhere) held it; its newest event 2026-10-15T05:11Z is past claim_ttl 4h (from /)
    $ lit get /next
    /next   bci   /lanes/scene-load 1/2   top-ranked lane; its claim expired

### A contested lane

Two checkouts each started a ticket in px1f before either saw the other. The holder
set has both; `claim` is the latest in total order and `contested` the rest.

    was: lit start 9u9   →   claimed here: … contested by stream ufijgjuy …
    $ lit get /lanes/px1f
    /lanes/px1f  rank 2   claim stream voice-di1 (elsewhere) · 3h   contested this checkout
      9u9   impl
      xuv   backlog
      31y   backlog

### What next serves when every lane is held elsewhere

    was: lit next
    $ lit get /next
    /next  (none): every lane with an open head is held elsewhere
      /lanes/store-bugs   rank 1  head 9r8  stream ufijgjuy (elsewhere) · 2h
      /lanes/checkpoint   rank 2  head l1x  claude_f335… (elsewhere) · 40m
      /lanes/lm3          rank 3  head 7iy  stream 4c9d… (elsewhere) · 5m

### An urgent lane interrupts the one I hold

    $ lit set c44/priority urgent
    c44/priority  normal → urgent
    ⇒ /next = c44 (urgent head of /lanes/c44, rank 1, above /lanes/datetime held by this checkout)

### A claim on a lane versus on a solo ticket

    $ lit get /lanes/datetime hf4/lane hf4/claim
    /lanes/datetime  rank 4
      dee   impl   this checkout
      b64   backlog
      fw9   backlog
    hf4/lane    hf4
    hf4/claim   (none)

### Who am I writing as?

    was: lit whoami   →   unknown command "whoami"
    $ lit get /me
    checkout    7f2e…
    principal   claude_7f2e…  (contributor)
    agent       claude-code
    tiers       default

## Reading

### Read one ticket

    was: lit show promptctl-warnings-2d80.49k
    $ lit get 49k
    $ = promptctl-warnings-2d80.49k
    $/title          A warning printed twice in one turn is shown once
    $/status         merge
    $/parent         promptctl-warnings-2d80
    $/lane           49k
    $/claim          this checkout
    $/body           The bar dedupes by text, but two warnings with …
    $/plan           revision 3
    $/plan/verdict   pass

    guidance (merge): Once it has merged, close it.

### Read one field

    was: lit show links-plan-review-o3mh --field status
    $ lit get o3mh/status
    done

### Read a few fields of one ticket

    was: lit show brandon-unfiled-3248.9uht --field created_at,updated_at
    $ lit get 9uht/created_at 9uht/updated_at
    $ = brandon-unfiled-3248.9uht
    $/created_at   2026-10-19T22:52:43Z
    $/updated_at   2026-10-21T04:10:09Z

### Read only the body

    was: lit show houdini-laws-etg.2ka | sed -n '/^description:/,/^Epic:/p'
    $ lit get 2ka/body
    Too-large tool results go whole to a file, and the reply names the path …

### Pull the body out, edit it, write it back

    was: lit show promptctl-backlog-3vck.nhy | awk '/^description:/{f=1;next} /^Epic:/{f=0} f' > /tmp/nhy.md; lit update promptctl-backlog-3vck.nhy --description-file /tmp/nhy.md
    $ lit get nhy/body > /tmp/nhy.md
    $ lit set nhy/body - < /tmp/nhy.md
    nhy/body  v3 → v4

### Put back an earlier description

    was: lit history links-statusline-e9pl.yjx --version description@1 > /private/tmp/yjx-v1.txt
    $ lit get yjx/body/versions/1 > /private/tmp/yjx-v1.txt
    $ lit set yjx/body - < /private/tmp/yjx-v1.txt
    yjx/body  v4 → v5

### Read only the comments

    was: lit show brandon-unfiled-3248.8ro6 | sed -n '/^comments:/,$p'
    $ lit get 8ro6/comments
    $ = brandon-unfiled-3248.8ro6
    $/comments/43s   [claude_64e3… Oct 21] Facts from PR #43: the watcher reads …
    $/comments/7kq   [claude_9a1b… Oct 22] Plan review (digest f76132367de4): fail. …
    $/comments/b0e   [claude_9a1b… Oct 23] Plan v43 (digest a02badd67281) passed law review.

### Read one comment to quote it

    was: lit show orange-admin-helper-b2q.8ew | sed -n '/cmt-e28a7f6d/,/cmt-507e36b0/p'
    $ lit get 8ew/comments/e28
    [claude_41c0… Oct 24] The helper's install path is read from one place: …

### List an epic's children

    was: lit children candybar-browser-surface-97sw
    $ lit get 97sw/children
    [97sw.1c2, 97sw.d4e, 97sw.k9f, 97sw.p0s]

### See every child's status in one read

    was: lit children candybar-testing-0x6u --columns id,state,blocked,title | grep -v closed
    $ lit get '0x6u.*/status'
    0x6u.4bt/status   backlog
    0x6u.snf/status   impl
    0x6u.1ur/status   done
    0x6u.yzt/status   backlog

### Read an epic without its children's bodies

    was: lit show orange-alerts-cjt | sed -n '/^description:/,/^children:/p'
    $ lit get cjt
    $ = orange-alerts-cjt
    $/title      Alerts fire on threshold crossings and say what crossed
    $/stage      active
    $/rollup     backlog 1 · impl 1 · done 1
    $/body       Alerts today fire on every sample above the line …
    $/children   [cjt.3ns, cjt.7pq, cjt.v2a]
    $/lanes      [/lanes/alerts]

### Read a child without the epic reprinted under it

    was: lit show promptctl-testing-c3u.4xm | awk '/^Epic:/{exit} 1'
    $ lit get 4xm
    $ = promptctl-testing-c3u.4xm
    $/title      The replay test waits on the brain's own signal, not a sleep
    $/status     backlog
    $/parent     promptctl-testing-c3u
    $/lane       4xm
    $/body       test/replay.test.ts sleeps 2s and then asserts …

### Read a lane

    $ lit get /lanes/datetime
    /lanes/datetime  rank 4
      dee   impl   this checkout
      b64   backlog
      fw9   backlog

### Read several tickets in one call

    was: lit show orange-devices-bue.1e6; lit show orange-devices-bue.chz; lit show orange-devices-bue.hqi
    $ lit get 1e6 chz hqi
    $ = orange-devices-bue.1e6
    $/title    The device list shows a sweep in progress
    $/status   backlog
    …
    $ = orange-devices-bue.chz
    …
    $ = orange-devices-bue.hqi
    …

### Read the same fields across several tickets

    was: lit ls --ids houdini-laws-etg.0yy,houdini-laws-etg.y04 --columns id,state,assignee,updated_at --format table
    $ lit get 0yy/status 0yy/claim 0yy/updated_at y04/status y04/claim y04/updated_at
    0yy/status       impl
    0yy/claim        stream 4f1c09e2 (elsewhere)
    0yy/updated_at   2026-10-14T23:02:51Z
    y04/status       backlog
    y04/claim        (none)
    y04/updated_at   2026-10-12T08:40:17Z

### Check the status after a write

The write's echo already says it; this is the read a later session makes.

    was: lit done promptctl-warnings-2d80.49k; lit show promptctl-warnings-2d80.49k | grep -E "^(status|phase):"
    $ lit get 49k/status
    done

### Read what blocks a ticket

    was: lit show orange-devices-bue.2af 2>&1 | grep -A3 blocked_by
    $ lit get 2af/blocked_by
    [orange-device-sources-rec]

### Read the column graph a ticket resolves to

    was: lit workflows show promptctl-brain-q9s8.45d
    $ lit get 45d/columns
    45d/columns  /columns
      backlog   initial:true  next:[plan]  exit:[ready]
      plan      next:[impl]
      impl      next:[review]  entry:[plan/verdict=pass]
      …

### See who claimed a ticket and when

    was: lit history houdini-laws-etg.6op 2>&1 | tail -8
    $ lit get 6op/history
    6op/history/1   Oct 15 05:11  claude_214f…  status backlog → plan
    6op/history/2   Oct 15 05:40  claude_214f…  plan/revisions + 1
    6op/history/3   Oct 15 09:02  claude_8e0c…  plan/reviews + c71 pass

### Find claims nobody has touched in half a day

    was: lit orphaned
    $ lit get '(status=plan updated<-12h)/claim'
    houdini-laws-etg.bci/claim   claude_5d2e… (elsewhere)

## Reading many tickets in one call

### Title of every ticket, to see the backlog without 59 shows
The grooming pass ran one `show` per ticket and got ~270 KB back. Most of it was epic bodies, reprinted once per child.

    was: for id in $(lit ls --columns id | awk '{print $1}'); do lit show $id; done
    $ lit get '*/title'
    792a/title       Unfiled
    792a.dee/title   dateInZone accepts zone names in any letter case
    792a.b64/title   dateInZone accepts fixed offsets like +05:30
    792a.fw9/title   date parses RFC 3339 with fractional seconds
    rin/title        Subtemplates
    rin.2ae/title    define and template share one namespace
    …

### Status of every ticket
    was: for id in …; do lit show $id | grep '^status:'; done
    $ lit get '*/status'
    792a.dee/status   impl
    792a.b64/status   backlog
    792a.fw9/status   backlog
    rin.2ae/status    review
    rin.9u9/status    done
    …

### Title, status, and lane of every ticket, one line per field
    $ lit get '*/title' '*/status' '*/lane'
    792a.dee/title    dateInZone accepts zone names in any letter case
    792a.dee/status   impl
    792a.dee/lane     datetime
    792a.b64/title    dateInZone accepts fixed offsets like +05:30
    792a.b64/status   backlog
    792a.b64/lane     datetime
    rin.2ae/title     define and template share one namespace
    rin.2ae/status    review
    rin.2ae/lane      2ae
    …

### Status of three tickets in another checkout's lane
    was: for t in 5tv zi4 8l0; do lit show dy46.$t | sed -n '1,12p'; done
    $ lit get dy46.5tv/status dy46.zi4/status dy46.8l0/status
    dy46.5tv/status   done
    dy46.zi4/status   impl
    dy46.8l0/status   backlog

### Status of every child of an epic
    was: lit children candybar-testing-0x6u --columns id,state,blocked,title | grep -v closed
    $ lit get '0x6u.*/status'
    0x6u.snf/status   impl
    0x6u.1ur/status   backlog
    0x6u.4bt/status   backlog
    0x6u.yzt/status   done
    0x6u.5bi/status   dup

### Bodies of four tickets, without the epic tail
The old read was `sed -n '/^description:/,/^Epic:/p'`, once per ticket.

    was: for t in 2ka 4id y04 0yy; do lit show etg.$t | sed -n '/^description:/,/^Epic:/p'; done
    $ lit get etg.2ka/body etg.4id/body etg.y04/body etg.0yy/body
    etg.2ka/body   Too-large tool results go whole to a file …
    etg.4id/body   find_error_nodes skips summarization when …
    etg.y04/body   The scene loader reports a missing asset as …
    etg.0yy/body   Connections time out after 30 s with no …

### Head of one ticket: title, status, body, what blocks it
Several paths on one ticket print the ticket once, as `$`.

    was: lit show dy46.5tv | sed -n '1,12p;/^description:/,/^blocked_by:/p'
    $ lit get dy46.5tv/title dy46.5tv/status dy46.5tv/blocked_by dy46.5tv/body
    $ = links-scale-dy46.5tv
    $/title        Backlog reads stay under 200 ms at 10k tickets
    $/status       backlog
    $/blocked_by   bryp
    $/body         The backlog query scans every issue_history row …

### Comments on one ticket, without the rest of the ticket
    was: lit show 3248.8ro6 | sed -n '/^comments:/,$p' | cut -c1-600
    $ lit get 3248.8ro6/comments
    $ = brandon-unfiled-3248.8ro6
    $/comments/43s   [claude_64e3…] Plan v21 recorded; digest f7613236 …
    $/comments/9kq   [claude_a1f9…] Plan review (digest f76132367de4): fail. …
    $/comments/2mw   [claude_64e3…] Plan v22 answers findings 1–3 …

### One comment, to quote it in a follow-up
    was: lit show b2q.8ew | sed -n '/cmt-e28a7f6d/,/cmt-507e36b0/p'
    $ lit get b2q.8ew/comments/e28
    [claude_9c02…] Plan review (digest 56fa5f1faa63): fail. 1. one-source-of-truth: …

### Titles of the children of five epics
    was: for e in 6pj 1dh f00 5d8 b2l; do lit ls --parent 76bb.$e; done
    $ lit get '76bb.6pj.*/title' '76bb.1dh.*/title' '76bb.f00.*/title' '76bb.5d8.*/title' '76bb.b2l.*/title'
    76bb.6pj.7q7/title   The gather renderer draws one row per source
    76bb.6pj.e1n/title   A source with no samples draws an empty row
    76bb.1dh.mxk/title   …
    …

### Parent of each ticket, before filing a sibling
    was: for t in …; do lit show $t | grep -A2 '^parent:' | sed -n 2p; done
    $ lit get hands-nativeui-02f/parent hands-install-dye.73t/parent hands-install-dye.8yk/parent
    hands-nativeui-02f/parent      (none)
    hands-install-dye.73t/parent   hands-install-dye
    hands-install-dye.8yk/parent   hands-install-dye

### Created and updated times of one ticket
    was: lit show 3248.9uht --field created_at,updated_at
    $ lit get 3248.9uht/created_at 3248.9uht/updated_at
    $ = brandon-unfiled-3248.9uht
    $/created_at   2026-10-19T22:52:43Z
    $/updated_at   2026-10-21T04:10:07Z

### Every open ticket's body in one file, to grep
    was: for id in $(lit ls --status open,in_progress --columns id | awk '{print $1}'); do lit show $id --no-plan > /tmp/groom/t-$id.txt; done
    $ lit get '(status=backlog)/body' > /tmp/groom/open-bodies.txt
    $ grep -n 'backoff' /tmp/groom/open-bodies.txt
    212:3k6u.ckq/body   … retries with exponential backoff capped at 30 s …

### History of every ticket carrying a label
    was: for id in $(lit ls --labels focus …| awk '{print $1}'); do lit history "$id" | grep -i -m1 label; done
    $ lit get '(labels=focus)/history'
    3k6u.knq/history   2026-10-20T22:52:43Z  claude_214f…  labels  [] → [focus]
    dy46.48k/history   2026-10-22T03:11:09Z  claude_96d7…  labels  [external] → [external, focus]

## Selectors

### Backlog tickets labelled bug, by title
`type` is not a field; a workspace that sorts bugs from the rest declares a `bug` label.

    was: lit ls --status open --type bug
    $ lit get '(status=backlog labels=bug)/title'
    3248.kler/title   daemon.log is ~90% 'schema: unknown field' lines
    xu4.8pq/title     A connected device sweeps with an empty name

### Every top-level item
The root's children are the top-level items; `type` is not a field.

    was: lit ls --type epic
    $ lit get /children
    [792a, rin, 6fq5, …]

### Children of an epic, by selector or by glob
`(parent=rin)` and `rin.*` expand to the same ids when every child sits directly under `rin`.

    was: lit ls --parent go-template-js-rin
    $ lit get '(parent=rin)/title'
    rin.2ae/title   define and template share one namespace
    rin.9u9/title   block falls back to its own body when undefined
    rin.k3x/title   template with no pipeline passes nil dot

### Dedupe before filing: has anyone already filed this?
Reviewers searched two or three terms before writing a ticket.

    was: lit ls --search powerd --status open,in_progress,closed; lit ls --search sweep --status open,in_progress,closed
    $ lit get '(text~"powerd")/status' '(text~"sweep")/status'
    bue.6on/status   review
    bue.1e6/status   done
    bue.chz/status   backlog

### Dedupe among the bugs, before filing one
    $ lit get '(text~"empty name" labels=bug)/title'
    xu4.8pq/title   A connected device sweeps with an empty name

### Orphaned: in flight, no update in six hours
`stage` is derived from the two flags the engine knows, so "in flight" is one key in every subtree's dialect.

    was: lit orphaned
    $ lit get '(stage=active updated<-6h)/claim'
    etg.bci/claim   claude_f335… (elsewhere)  claimed 12h ago

### Tickets touched since yesterday
    was: lit ls --updated-after 2026-11-02
    $ lit get '*/updated_at' | awk '$2 >= "2026-11-02"'
    792a.dee/updated_at   2026-11-02T19:40:11Z
    rin.2ae/updated_at    2026-11-03T03:02:55Z

### Tickets that have comments
    was: lit ls --query 'has:comments'
    $ lit get '(status=backlog)/comments'
    3248.8ro6/comments/43s   [claude_64e3…] Plan v21 recorded …
    3248.8ro6/comments/9kq   [claude_a1f9…] Plan review (digest f76132367de4): fail. …
    bue.6on/comments/1pa     [claude_7f2e…] powerd sleeps the sweep during a call …
    …

### First five closed tasks with comments, in rank order
    was: lit ls --query 'status:closed type:task has:comments sort:rank:asc limit:5'
    $ lit get '(status=done)/title'
    792a.2ku/title   dateInZone rejects a malformed offset
    792a.207/title   date accepts a Unix timestamp string
    …   (every one, in no order)

### Tickets parked out of the flow
There is no archive; a ticket out of the flow sits in a terminal column.

    was: lit ls --ids hands-nativeui-02f,hands-install-dye --include-archived
    $ lit get '(status=obsolete)/title'
    voice-r7y.8rr/title   Wake word survives a sleep/wake cycle

### Restore an epic deleted in error
`rm x` is a reversible tombstone; the examples write the reversal as `set x/deleted false`.

    was: lit restore vhid-aim-sjt
    $ lit set sjt/deleted false
    sjt/deleted  true → false
    ⇒ sjt.*/deleted  true → false  (3 children)

### Every ticket in review, at every depth, in each subtree's own column graph
`rin` adds an `audit` state. `(status=review)` still matches its review tickets, because a selector matches each node's resolved state.

    $ lit get '(status=review)/title'
    rin.2ae/title      define and template share one namespace
    bue.6on/title      powerd sleeps the sweep during a call
    3248.8ro6/title    Watchers derive their targets through one builder

### Open tickets except epics
    was: lit ls --status open | grep -v epic
    $ lit get '(status=backlog)/title'
    792a/title       Unfiled
    792a.b64/title   dateInZone accepts fixed offsets like +05:30
    rin/title        Subtemplates
    …   (the epics are in it)

### A named set of unrelated ids, two fields each
    was: lit ls --ids etg.0yy,etg.y04 --columns id,state,assignee,updated_at
    $ lit get etg.0yy/status etg.0yy/claim etg.y04/status etg.y04/claim
    etg.0yy/status   impl
    etg.0yy/claim    claude_214f… (worktree etg-0yy)
    etg.y04/status   backlog
    etg.y04/claim    (none)

## Comments

### Leave a short note

    was: lit comment add orange-widgets-5jo.nlf --body "From PR #49 review: decide whether a level's form …"
    $ lit add nlf/comments "From PR #49 review: decide whether a level's form …"
    + nlf/comments/k2m

### Leave a long note from a heredoc

    was: lit comment add candybar-animation-wbxz.81o.2jn --body-file - <<'EOF'
    $ lit add 81o.2jn/comments - <<'EOF'
    Answered: stop() kills the child. On 2.1.299 the child outlives the mod by one tick …
    EOF
    + 81o.2jn/comments/p3d

### Remove a comment

    was: lit comment rm cmt-563ec2d8-…
    $ lit rm b06/comments/563
    - b06/comments/563

### Remove two stale comments in one call

    was: lit comment rm cmt-2f277e42-… && lit comment rm cmt-f5f02379-…
    $ lit rm 03v/comments/2f2 03v/comments/f5f
    - 03v/comments/2f2
    - 03v/comments/f5f

### Correct an earlier comment by appending a new one

    was: lit comment add mock-mock-flu.3h8 --body "Correction to cmt-03d4a989: …"
    $ lit add 3h8/comments "Correction to 3h8/comments/03d: the credential file is src/oauth.ts, not src/login.ts."
    + 3h8/comments/v6n

### Leave a note on a ticket this session does not own

    was: lit comment add mock-mock-flu.aus --body "From mock-mock-flu.86x: the credential login writes is src/oauth.ts …"
    $ lit add aus/comments "From flu.86x: the credential login writes is src/oauth.ts …"
    + aus/comments/j1t

## Notes between agents

### Only one author's comments

    $ lit get '8ew/comments/(by=claude_96d7)/body'
    8ew/comments/e28a/body   Plan review (digest 56fa5f1faa63): fail. …
    8ew/comments/507e/body   Plan review (digest 9c01e3a7b2f4): pass.

## Filing

### File a bug under an epic, body from stdin

    was: lit new --type bug --topic devices --parent orange-devices-bue --title "A connected device sweeps with an empty name" --description-file - <<'EOF'
    $ lit add bue/children '{title:"A connected device sweeps with an empty name" body:-}' <<'EOF'
    Repro: pair a device with no name set; the sweep row renders "" …
    EOF
    + bue.k7r

### File a task with a one-line body

    was: lit new --parent promptctl-evals-bgz --type task --topic isolation --title "The Opus verification reads what the session runs" --description "…"
    $ lit add bgz/children '{title:"The Opus verification reads what the session runs, not what the model says it is" body:"Done looks like: …"}'
    + bgz.10

### File three children in one call

    was: for t in one two three; do lit new --parent $E --topic wake-word --title "ticket $t"; done
    $ lit add i15t/children '[{title:"Wake word fires on the first syllable"}, {title:"Wake word ignores the TV"}, {title:"Wake word logs each miss"}]'
    + i15t.r2a
    + i15t.r2b
    + i15t.r2c

### File a follow-up beside a closed ticket, in its epic

    was: lit followup --on orange-settings-7du.5uw --title "The app quit by itself twice right after the Settings window opened" --type bug
    $ lit get 5uw/parent
    orange-settings-7du
    $ lit add 7du/children '{title:"The app quit by itself twice right after the Settings window opened"}'
    + 7du.a2c

### File a top-level epic

    was: lit new --type epic --topic history --title "History reads come from one store"
    $ lit add /children '{title:"History reads come from one store"}'
    + orange-m2q

### A leaf filed with no parent lands in the synthetic home

`add /children` with no level creates at the top; naming level 1 files a leaf, and
it lands in the home the workspace file names for the level above it.

    was: lit new --type bug --topic daemon-git-identity --title "A daemon serving sessions …"
    $ lit add /children '{level:1 title:"A daemon serving sessions commits as the wrong identity"}'
    + unfiled.k4e
    ⇒ k4e/parent = unfiled

## Bulk create

### Create an epic's tickets from one list on stdin
    was: lit import --path /tmp/w0o-groom/tickets.yaml
    $ lit add flu/children - <<'EOF'
    [{title:"Login writes the credential where the refresh reads it"},
     {title:"The refresh retries once on a 401, then surfaces it"},
     {title:"Token expiry is read from the token, not assumed"}]
    EOF
    + flu.8xa
    + flu.8xb
    + flu.8xc

### Create with bodies and labels, values quoted where they contain a colon
The YAML import failed on `mapping values are not allowed`. In a value, a colon inside quotes is text.

    $ lit add cb/children - <<'EOF'
    [{title:"Vocab: one word per gauge" labels:[refine] body:"Done when: every gauge label is one word."},
     {title:"Vocab: units in the legend, not the label" labels:[refine]}]
    EOF
    + cb.v1a
    + cb.v1b

### Dry-run a bulk create to check every item before minting ids
    $ lit add 0x6u/children - --dry-run < /tmp/epic-tickets.list
    + 0x6u/children  {title:"launchWaited returns after the first frame"}
    + 0x6u/children  {title:"The fake store answers schema v12"}
    + 0x6u/children  {title:"…" labels:[bugg]}   refused: label bugg is not declared in the workspace file
    (dry run: nothing written)

### Create a new epic and its children in one call
    was: lit import --path /tmp/ccb-tickets/candybar.yaml   (created 18 issues / tmux-epic -> candybar-tmux-events-y457)
    $ lit add /children - <<'EOF'
    [{title:"tmux server events"
      children:[{title:"Parse %output notifications"},
                {title:"Reconnect after the server restarts"}]}]
    EOF
    + y457
    + y457.a1
    + y457.a2

### Create tickets that depend on each other in one call
The YAML import used local keys (`plan-1 -> candybar-testing-ihvy.myy`) so one new ticket could block another.

    $ lit add ihvy/children - < /tmp/epic-tickets.list
    + ihvy.myy
    + ihvy.myz
    $ lit add ihvy.myy/blocks ihvy.myz
    + ihvy.myy/blocks/ihvy.myz

### Update many existing tickets from one list
The import help said a document with an `id` updates that ticket. Updating is `set`, with pairs.

    was: lit import --path /tmp/groom/lanes.yaml   (after two failed attempts to generate the YAML)
    $ lit set px1f.f9b/priority urgent px1f.9u9/priority urgent px1f.xuv/parent lq3
    px1f.f9b/priority   normal → urgent
    px1f.9u9/priority   normal → urgent
    px1f.xuv/parent     px1f → lq3

## Closing

### Close a merged ticket in a close-out session

    was: lit done promptctl-warnings-2d80.49k --reason "PR #332 merged as f4cfaa0"
    $ lit set 49k/status done 49k/reason "PR #332 merged as f4cfaa0"
    49k/status  merge → done
    49k/reason  "" → "PR #332 merged as f4cfaa0"
    ⇒ 49k/claim  this checkout → (none)
    ⇒ /lanes/49k  finished; this checkout holds no lane

### Close several merged tickets at once

    was: for t in 2ku 207 oq5; do lit done template-unfiled-792a.$t; done
    $ lit set 2ku/status done 207/status done oq5/status done
    2ku/status  merge → done
    207/status  merge → done
    oq5/status  merge → done
    ⇒ 2ku/claim  this checkout → (none)
    ⇒ 207/claim  this checkout → (none)
    ⇒ oq5/claim  this checkout → (none)

### Close as won't-fix, with the reason

    was: lit close houdini-install-aby.80r --resolution wontfix --reason "…"
    $ lit set 80r/status wontfix 80r/reason "Installing into the user's Houdini prefs is theirs to do; the README says how."
    80r/status  backlog → wontfix
    80r/reason  "" → "Installing into the user's Houdini prefs is theirs to do; the README says how."

### Won't-fix with no reason is refused

    $ lit set 80r/status wontfix
    error (reason=requires): /columns/wontfix requires 80r/reason, which is empty; nothing written

### Close as obsolete, with the reason

    was: lit close promptctl-hierarchy-6cqf.2la --resolution obsolete --reason "groom: its work (a migration dropping issue_type) is ruled out by …"
    $ lit set 2la/status obsolete 2la/reason "groom: its work (a migration dropping issue_type) is ruled out by …"
    2la/status  backlog → obsolete
    2la/reason  "" → "groom: its work (a migration dropping issue_type) is ruled out by …"

### Close as a duplicate of the canonical ticket

    was: lit close promptctl-warnings-2d80.d66 --resolution duplicate --of promptctl-brain-lq3.9od
    $ lit set d66/status dup d66/of lq3.9od
    d66/status  backlog → dup
    d66/of      "" → promptctl-brain-lq3.9od

### A duplicate with no canonical is refused

    $ lit set d66/status dup
    error (reason=requires): /columns/dup requires d66/of, which is empty; nothing written

### Close three duplicates into one canonical

    was: for t in d66 c7e 61x; do lit close promptctl-warnings-2d80.$t --resolution duplicate --of promptctl-brain-lq3.9od; done
    $ lit set d66/status dup d66/of lq3.9od c7e/status dup c7e/of lq3.9od 61x/status dup 61x/of lq3.9od
    d66/status  backlog → dup
    d66/of      "" → promptctl-brain-lq3.9od
    c7e/status  backlog → dup
    c7e/of      "" → promptctl-brain-lq3.9od
    61x/status  backlog → dup
    61x/of      "" → promptctl-brain-lq3.9od

### Close as superseded by the ticket that replaced it

    was: lit close houdini-laws-etg.2ka --resolution superseded --of houdini-laws-etg.6op --reason "user decided: too-large results go whole to a file …"
    $ lit set 2ka/status superseded 2ka/of 6op 2ka/reason "user decided: too-large results go whole to a file …"
    2ka/status  backlog → superseded
    2ka/of      "" → houdini-laws-etg.6op
    2ka/reason  "" → "user decided: too-large results go whole to a file …"

### Close an abandoned epic's open children with one reason, previewed first

    was: lit close dotfiles-iterm2-restore-5k5.4.1 --resolution obsolete; lit close dotfiles-iterm2-restore-5k5 --resolution obsolete
    $ lit set '(parent=5k5 status=backlog)/status' obsolete '(parent=5k5 status=backlog)/reason' "iTerm2 restore dropped" --dry-run
    5k5.4.1/status  backlog → obsolete
    5k5.4.1/reason  "" → "iTerm2 restore dropped"
    5k5.4.2/status  backlog → obsolete
    5k5.4.2/reason  "" → "iTerm2 restore dropped"
    (dry run: nothing written)

### Reopen a closed ticket

    was: lit open sanctuary-render-tgx.rkm --reason "Done-when fails: the bump has not landed"
    $ lit set rkm/status backlog rkm/reason "Done-when fails: the bump has not landed"
    rkm/status  done → backlog
    rkm/reason  "Merged in #212" → "Done-when fails: the bump has not landed"

### Park an in-flight ticket back in backlog and release its claim

    was: lit open links-scale-dy46.37o --reason "parked behind promptctl-sync-bryp"
    $ lit set 37o/status backlog 37o/reason "parked behind promptctl-sync-bryp"
    37o/status  impl → backlog
    37o/reason  "" → "parked behind promptctl-sync-bryp"
    ⇒ 37o/claim  this checkout → (none)
    ⇒ /lanes/37o  held by (none)

## Labels, parents, dependencies

### Add a label

    was: lit label add links-perf-kw6z.5 external
    $ lit add kw6z.5/labels external
    + kw6z.5/labels/external

### Remove a label, then start the ticket

    was: lit label rm candybar-effort-gauge-9to external && lit start candybar-effort-gauge-9to
    $ lit rm 9to/labels/external
    - 9to/labels/external
    $ lit set 9to/status plan
    9to/status  backlog → plan
    ⇒ 9to/claim = this checkout

### Clear the label from the epics the pass finished

    was: for e in orange-history-4y3 orange-devices-bue orange-iphone-watch-lm3; do lit label rm $e decompose; done
    $ lit rm 4y3/labels/decompose bue/labels/decompose lm3/labels/decompose
    - 4y3/labels/decompose
    - bue/labels/decompose
    - lm3/labels/decompose

### Move a ticket under a different epic

    was: lit parent set --child links-unfiled-fc79.rm9w --parent promptctl-testing-0oqd
    $ lit set rm9w/parent 0oqd
    rm9w/parent  links-unfiled-fc79 → promptctl-testing-0oqd

### Move a ticket under an epic that carries its own column graph

    $ lit set q4p/parent lq3
    q4p/parent  (none) → promptctl-brain-lq3
    ⇒ q4p/columns  /columns → promptctl-brain-lq3/columns

### Move three tickets under one epic

    was: for t in bz6 3cz ths; do lit parent set --child tmux-pane-detail-aha.$t --parent tmux-capture-restore-99o; done
    $ lit set bz6/parent 99o 3cz/parent 99o ths/parent 99o
    bz6/parent  tmux-pane-detail-aha → tmux-capture-restore-99o
    3cz/parent  tmux-pane-detail-aha → tmux-capture-restore-99o
    ths/parent  tmux-pane-detail-aha → tmux-capture-restore-99o

### Make a ticket wait on one in another epic

    was: lit dep add links-scale-dy46.37o --blocked-by promptctl-sync-bryp
    $ lit add bryp/blocks 37o
    + bryp/blocks/37o
    ⇒ 37o/blocked_by  [] → [promptctl-sync-bryp]

### Make five tickets wait on one

    was: for t in 0jy 04u vap 73s 7v5; do lit dep add houdini-scene-iteration-jcj.$t --blocked-by houdini-scene-iteration-jcj.l1e; done
    $ lit add l1e/blocks '[0jy, 04u, vap, 73s, 7v5]'
    + l1e/blocks/0jy
    + l1e/blocks/04u
    + l1e/blocks/vap
    + l1e/blocks/73s
    + l1e/blocks/7v5
    ⇒ 0jy/blocked_by  [] → [houdini-scene-iteration-jcj.l1e]
    ⇒ 04u/blocked_by  [] → [houdini-scene-iteration-jcj.l1e]
    ⇒ vap/blocked_by  [] → [houdini-scene-iteration-jcj.l1e]
    ⇒ 73s/blocked_by  [] → [houdini-scene-iteration-jcj.l1e]
    ⇒ 7v5/blocked_by  [] → [houdini-scene-iteration-jcj.l1e]

### Make one ticket wait on a sibling in the same epic

The old CLI refused this ("Do not set 'blocks' relationships between two issues in the same epic").

    was: lit dep add --from copirate-efficiency-235.1 --to copirate-efficiency-235.2 --type blocks
    $ lit add 235.1/blocks 235.2
    + 235.1/blocks/235.2
    ⇒ 235.2/blocked_by  [] → [copirate-efficiency-235.1]

### Remove a dependency

    was: lit dep rm houdini-connections-ob5 --blocked-by houdini-scene-iteration-jcj.l1e
    $ lit rm l1e/blocks/ob5
    - l1e/blocks/ob5
    ⇒ ob5/blocked_by  [houdini-scene-iteration-jcj.l1e] → []

## Several fields on one ticket

### Title and body in one call
    was: lit update e9pl.9ym --title "…" --type task --description-file -
    $ lit set e9pl.9ym/title "The statusline names the lane it serves" e9pl.9ym/body - <<'EOF'
    The statusline shows the lane's head and its position (2/3).
    EOF
    e9pl.9ym/title   The statusline shows rank → The statusline names the lane it serves
    e9pl.9ym/body    v4 → v5  (+1 −6 lines)

### Replace a ticket's labels, seeing what was removed
    was: lit label rm 9to external && lit label add 9to needs-design
    $ lit set 9to/labels '[needs-design]'
    9to/labels   [external] → [needs-design]
    - 9to/labels/external

### Parent and priority together
    $ lit set rm9w/parent 0oqd rm9w/priority urgent
    rm9w/parent     fc79 → 0oqd
    rm9w/priority   normal → urgent

## Bulk writes, dry run first

### Set priority on every CI-filed alarm, shown before it is done
    was: for id in z6sv.dbc z6sv.463 z6sv.bd3 z6sv.ix9 z6sv.86e; do lit update $id --priority normal --reason "groom: …"; done
    $ lit set '(parent=z6sv priority=urgent)/priority' normal --dry-run
    (parent=z6sv priority=urgent) = [z6sv.dbc, z6sv.463, z6sv.bd3, z6sv.ix9, z6sv.86e]
    z6sv.dbc/priority   urgent → normal
    z6sv.463/priority   urgent → normal
    z6sv.bd3/priority   urgent → normal
    z6sv.ix9/priority   urgent → normal
    z6sv.86e/priority   urgent → normal
    (dry run: nothing written)
    $ lit set '(parent=z6sv priority=urgent)/priority' normal
    z6sv.dbc/priority   urgent → normal
    z6sv.463/priority   urgent → normal
    z6sv.bd3/priority   urgent → normal
    z6sv.ix9/priority   urgent → normal
    z6sv.86e/priority   urgent → normal

### Urgent on an epic and every child
    was: for id in w1p w1p.1 w1p.2 w1p.3; do lit update macklebox-safety-$id --priority 0; done
    $ lit set w1p/priority urgent 'w1p.*/priority' urgent
    w1p/priority     normal → urgent
    w1p.1/priority   normal → urgent
    w1p.2/priority   normal → urgent
    w1p.3/priority   normal → urgent

### Dependency fan-out: seven tickets wait on one blocker
    was: lit dep add lm3.fyh --blocked-by rec; lit dep add lm3.u33 --blocked-by rec; … (7 calls)
    $ lit add rec/blocks '[lm3.fyh, lm3.u33, lm3.cn0, lm3.7iy, lm3.8pk, lm3.1tt, lm3.497]'
    + rec/blocks/lm3.fyh
    + rec/blocks/lm3.u33
    + rec/blocks/lm3.cn0
    + rec/blocks/lm3.7iy
    + rec/blocks/lm3.8pk
    + rec/blocks/lm3.1tt
    + rec/blocks/lm3.497
    ⇒ lm3.fyh/blocked_by   [] → [rec]
    ⇒ lm3.u33/blocked_by   [] → [rec]
    …

### One ticket waits on two blockers
    $ lit add 3248.kler/blocks sjhy.ju8
    + 3248.kler/blocks/sjhy.ju8
    $ lit add bryp/blocks sjhy.ju8
    + bryp/blocks/sjhy.ju8

### Swap the blocker on two tickets
    was: for t in bsog.sfi 7qoh.8nx; do lit dep rm $t --blocked-by uoz0.nx9; lit dep add $t --blocked-by px1f.e4u; done
    $ lit rm uoz0.nx9/blocks/bsog.sfi uoz0.nx9/blocks/7qoh.8nx
    - uoz0.nx9/blocks/bsog.sfi
    - uoz0.nx9/blocks/7qoh.8nx
    $ lit add px1f.e4u/blocks '[bsog.sfi, 7qoh.8nx]'
    + px1f.e4u/blocks/bsog.sfi
    + px1f.e4u/blocks/7qoh.8nx

### Drop one dependency
    was: lit dep rm houdini-connections-ob5 --blocked-by jcj.l1e
    $ lit rm jcj.l1e/blocks/ob5
    - jcj.l1e/blocks/ob5
    ⇒ ob5/blocked_by   [jcj.l1e] → []

### Reparent twelve tickets
    was: for p in "bij aj0" "o4f aj0" "czk aj0" …; do set -- $p; lit parent set --child flu.$1 --parent flu.$2; done
    $ lit set flu.bij/parent flu.aj0 flu.o4f/parent flu.aj0 flu.czk/parent flu.aj0 flu.w0o/parent flu.aj0
    flu.bij/parent   flu → flu.aj0
    flu.o4f/parent   flu → flu.aj0
    flu.czk/parent   flu → flu.aj0
    flu.w0o/parent   flu → flu.aj0

### Move every child of one epic under another
    was: for t in aha.bz6 aha.3cz aha.ths; do lit parent set --child $t --parent tmux-capture-restore-99o; done
    $ lit set 'aha.*/parent' 99o --dry-run
    aha.* = [aha.bz6, aha.3cz, aha.ths]
    aha.bz6/parent   aha → 99o
    aha.3cz/parent   aha → 99o
    aha.ths/parent   aha → 99o
    (dry run: nothing written)

### Clear a label from every epic that still has it
    $ lit rm '(labels=decompose)/labels/decompose'
    - kg2/labels/decompose
    - cjt/labels/decompose

### Close stale tickets as obsolete, each with the reason obsolete requires
    was: for t in etg.8qa etg.3d4 etg.ugm etg.aj3 2pq; do lit close $t --resolution obsolete --reason "ws_client deleted in #55"; done
    $ lit set etg.8qa/status obsolete etg.8qa/reason "ws_client deleted in #55" etg.3d4/status obsolete etg.3d4/reason "ws_client deleted in #55" etg.ugm/status obsolete etg.ugm/reason "ws_client deleted in #55"
    etg.8qa/status   backlog → obsolete
    etg.8qa/reason   (none) → ws_client deleted in #55
    etg.3d4/status   backlog → obsolete
    etg.3d4/reason   (none) → ws_client deleted in #55
    etg.ugm/status   backlog → obsolete
    etg.ugm/reason   (none) → ws_client deleted in #55

### Close every ticket matching a selector as obsolete, with the reason it requires
    $ lit set '(text~"ws_client" status=backlog)/status' obsolete '(text~"ws_client" status=backlog)/reason' "ws_client deleted in #55" --dry-run
    (text~"ws_client" status=backlog) = [etg.8qa, etg.3d4, etg.ugm, etg.aj3, 2pq]
    etg.8qa/status   backlog → obsolete
    etg.8qa/reason   (none) → ws_client deleted in #55
    …
    (dry run: nothing written)

### Mark three merged tickets done
    was: for t in 2ku 207 oq5; do lit done template-unfiled-792a.$t; done
    $ lit set 792a.2ku/status done 792a.207/status done 792a.oq5/status done
    792a.2ku/status   merge → done
    ⇒ 792a.2ku/claim  this checkout → (none)
    792a.207/status   merge → done
    ⇒ 792a.207/claim  this checkout → (none)
    792a.oq5/status   merge → done
    ⇒ 792a.oq5/claim  this checkout → (none)

### Close everything sitting in merge, after checking what that is
    $ lit set '(status=merge)/status' done --dry-run
    (status=merge) = [lq3.808, backlog-q4p, lq3.hwi, lq3.1dt]
    lq3.808/status       merge → done
    backlog-q4p/status   merge → done
    lq3.hwi/status       merge → done
    lq3.1dt/status       merge → done
    (dry run: nothing written)

### Reopen several tickets in one call
    was: lit open 2bs 2bs.tsm 2bs.99c   (refused: usage: lit open <id>)
    $ lit set 2bs.tsm/status backlog 2bs.99c/status backlog
    2bs.tsm/status   done → backlog
    2bs.99c/status   done → backlog

### Park every child of an abandoned epic in a terminal column
There is no archive flag; out of the flow is a terminal column, and `obsolete` requires a reason.

    was: for id in …; do lit archive $id --reason "voice dropped"; done
    $ lit set 'voice-r7y.*/status' obsolete 'voice-r7y.*/reason' "voice dropped" --dry-run
    voice-r7y.* = [voice-r7y.8rr, voice-r7y.ux7, voice-r7y.k2q]
    voice-r7y.8rr/status   backlog → obsolete
    voice-r7y.8rr/reason   (none) → voice dropped
    voice-r7y.ux7/status   backlog → obsolete
    voice-r7y.ux7/reason   (none) → voice dropped
    voice-r7y.k2q/status   impl → obsolete
    voice-r7y.k2q/reason   (none) → voice dropped
    ⇒ voice-r7y/stage = terminal
    (dry run: nothing written)

### Delete two probe tickets
    was: lit delete ef3.9yn --reason "leftover probe"; lit delete ef3.vqr --reason "filed in error"
    $ lit rm ef3.9yn ef3.vqr
    - ef3.9yn
    - ef3.vqr

### Purge an abandoned epic's whole subtree
    was: for id in 3aj.8k1 3aj.evr …; do lit purge $id --yes; done
    $ lit rm '76j.3aj.*' --purge --dry-run
    76j.3aj.* = [76j.3aj.8k1, 76j.3aj.evr, 76j.3aj.q0p]
    - 76j.3aj.8k1   (comments 3, labels 0, edges 2)
    - 76j.3aj.evr   (comments 0, labels 1, edges 1)
    - 76j.3aj.q0p   (comments 1, labels 0, edges 0)
    (dry run: nothing written)

### Remove two stale comments, then add the corrected one
    was: lit comment rm cmt-2f277e42-…; lit comment rm cmt-f5f02379-…; lit comment add flu.03v --body "…"
    $ lit rm flu.03v/comments/2f2 flu.03v/comments/f50
    - flu.03v/comments/2f2
    - flu.03v/comments/f50
    $ lit add flu.03v/comments "Login writes src/oauth.ts; the token refresh is in src/refresh.ts."
    + flu.03v/comments/7hd

### Remove one of a pair of identical comments posted to two tickets
    was: lit comment rm cmt-40f73118-…
    $ lit rm c3u.isp/comments/40f
    - c3u.isp/comments/40f

### Remove every comment one session left on a ticket
    $ lit rm 'ef3.b06/comments/*' --dry-run
    ef3.b06/comments/* = [a1k, a1m, c9p]
    - ef3.b06/comments/a1k
    - ef3.b06/comments/a1m
    - ef3.b06/comments/c9p
    (dry run: nothing written)

### The same comment on every child of an epic
    $ lit add '0x6u.*/comments' "Groom 2026-11-03: re-verified against master 2d7ee77."
    + 0x6u.snf/comments/m1c
    + 0x6u.1ur/comments/m1d
    + 0x6u.4bt/comments/m1e

### The same comment on three unrelated tickets
    was: for t in c3u.isp fxnj.cjq q9s8.qsk; do lit comment add $t --body "Fixed by #438 …"; done
    $ lit add c3u.isp/comments "Fixed by #438 (c3u.89i): the doubled emptying row came from the stale buffer."
    + c3u.isp/comments/q2a
    $ lit add fxnj.cjq/comments "Fixed by #438 (c3u.89i): the doubled emptying row came from the stale buffer."
    + fxnj.cjq/comments/q2b
    $ lit add q9s8.qsk/comments "Fixed by #438 (c3u.89i): the doubled emptying row came from the stale buffer."
    + q9s8.qsk/comments/q2c

### Eleven different grooming notes on eleven tickets
    was: lit comment add vu2t.7ro --body 'Grooming 2026-10-28: …'; lit comment add i15t.qg8 --body '…'; … (11 in one command)
    $ lit add vu2t.7ro/comments "Grooming 2026-10-28: superseded by the lq3 lane; kept for the repro."
    + vu2t.7ro/comments/b4n

### Rewrite six descriptions from a directory of files
    was: for t in b06 9e3 avb 0ho ll5 bl1; do lit update ef3.$t --description-file /tmp/groom-7/desc/$t.md --reason "groom: …"; done
    $ lit set ef3.b06/body "$(cat /tmp/groom-7/desc/b06.md)" ef3.9e3/body "$(cat /tmp/groom-7/desc/9e3.md)" ef3.avb/body "$(cat /tmp/groom-7/desc/avb.md)"
    ef3.b06/body   v3 → v4  (+12 −5 lines)
    ef3.9e3/body   v1 → v2  (+8 −8 lines)
    ef3.avb/body   v5 → v6  (+3 −1 lines)

### One description from stdin, the common case
    was: lit update 8x3 --description-file /tmp/groom-scope/8x3.txt
    $ lit set iwpa.8x3/body - < /tmp/groom-scope/8x3.txt
    iwpa.8x3/body   v2 → v3  (+14 −9 lines)

## Lanes

### Three new tickets that must be worked in order, as one lane
Tickets are created under their parent; a lane's members are ids. Two calls, and
the order given is the order in the lane. A new lane ranks last.

    $ lit add 792a/children - <<'EOF'
    [{title:"dateInZone accepts zone names in any letter case"},
     {title:"dateInZone accepts fixed offsets like +05:30"},
     {title:"date parses RFC 3339 with fractional seconds"}]
    EOF
    + 792a.dee
    + 792a.b64
    + 792a.fw9
    $ lit add /lanes/datetime '[dee, b64, fw9]'
    + /lanes/datetime  [dee, b64, fw9]
    ⇒ b64/blocked_by = dee
    ⇒ fw9/blocked_by = b64
    ⇒ /lanes/datetime ranks last (9 of 9)

### A lane from three existing tickets
    was: for t in 9r8 hf4 a53; do lit update 3k6u.$t --lane store-bugs --reason "independent of the checkpoint chain"; done
    $ lit add /lanes/store-bugs '[3k6u.9r8, 3k6u.hf4, 3k6u.a53]'
    + /lanes/store-bugs  [3k6u.9r8, 3k6u.hf4, 3k6u.a53]
    ⇒ 3k6u.9r8/lane = store-bugs
    ⇒ 3k6u.hf4/lane = store-bugs
    ⇒ 3k6u.a53/lane = store-bugs
    ⇒ 3k6u.hf4/blocked_by = 3k6u.9r8
    ⇒ 3k6u.a53/blocked_by = 3k6u.hf4

### Add one existing ticket to the end of a lane
    was: lit update 3k6u.324 --lane store-bugs
    $ lit add /lanes/store-bugs 3k6u.324
    + /lanes/store-bugs/3k6u.324
    ⇒ 3k6u.324/lane = store-bugs
    ⇒ 3k6u.324/blocked_by = 3k6u.a53

### Put every child of an epic into one lane, in their current order
    was: for t in 5x1 cfm; do lit update e5r4.$t --lane policy; done
    $ lit add /lanes/policy '[e5r4.5x1, e5r4.cfm]'
    + /lanes/policy  [e5r4.5x1, e5r4.cfm]
    ⇒ e5r4.cfm/blocked_by = e5r4.5x1

### Which lane a ticket is in
    was: lit show 3k6u.9r8 --field lane
    $ lit get 3k6u.9r8/lane
    store-bugs

### A ticket in no lane is its own lane
It lives at `/lanes/<id>`, so it has a rank path like any other lane.

    $ lit get e9pl.7a2/lane
    7a2
    $ lit get /lanes/7a2
    /lanes/7a2  rank 6
      7a2   backlog

### Reorder within a lane
`b64` turned out to be the foundation `dee` builds on.

    $ lit set /lanes/datetime '[b64, dee, fw9]'
    /lanes/datetime   [dee, b64, fw9] → [b64, dee, fw9]
    ⇒ b64/blocked_by   dee → (none)
    ⇒ dee/blocked_by   (none) → b64
    ⇒ fw9/blocked_by   b64 → dee
    ⇒ /lanes/datetime  head is now b64

### Reorder with a typo, caught by the dry run
The list is replaced, not merged, so a dropped id is a removal, and the echo names it.

    $ lit set /lanes/datetime '[b64, fw9]' --dry-run
    /lanes/datetime   [dee, b64, fw9] → [b64, fw9]
    - /lanes/datetime/dee
    ⇒ dee/lane  datetime → dee
    ⇒ b64/blocked_by   dee → (none)
    (dry run: nothing written)
    $ lit set /lanes/datetime '[b64, dee, fw9]'
    /lanes/datetime   [dee, b64, fw9] → [b64, dee, fw9]
    …

### Take two tickets out of a lane so they run in parallel
The groomer found that `help.py`, `scene.py` and `code_policy.py` fixes share no files. The lane was serializing them falsely.

    was: lit update etg.qeo --lane help --reason "groom: … share no files; the unlaned default lane serialized them falsely"
    $ lit rm /lanes/etg-fixes/etg.qeo /lanes/etg-fixes/etg.j33
    - /lanes/etg-fixes/etg.qeo
    - /lanes/etg-fixes/etg.j33
    ⇒ etg.qeo/lane  etg-fixes → qeo
    ⇒ etg.j33/lane  etg-fixes → j33

### Move a ticket from one lane to another
A ticket is in at most one lane, so adding it to another moves it, and the echo shows the removal.

    $ lit add /lanes/store-bugs 9r8
    - /lanes/checkpoint/9r8
    + /lanes/store-bugs/9r8
    ⇒ 9r8/lane  checkpoint → store-bugs
    ⇒ 3sm/blocked_by  9r8 → l1x

### Dissolve the lane every Unfiled ticket was stuck in
"Every ticket here is its own lane" was written in the Unfiled epic's description. Eight `--lane` calls were the fix. Removing the lane makes each member solo.

    was: for t in 1kx aki …; do lit update ihvy.$t --lane ihvy.$t; done
    $ lit rm /lanes/unfiled --dry-run
    - /lanes/unfiled   [2ku, 207, oq5, dee, b64, fw9, 7nq0, hpxb]
    ⇒ 2ku/lane  unfiled → 2ku
    ⇒ 207/lane  unfiled → 207
    …
    (dry run: nothing written)

### Rerank a whole lane
Everything in the lane moves together. Rank is written as a relation to another
lane, `above`, `below`, `top` or `bottom`, so two writers reranking offline both
still mean what they said.

    was: lit rank 3k6u.9r8 --top; lit rank 3k6u.hf4 --below 3k6u.9r8; lit rank 3k6u.a53 --below 3k6u.hf4
    $ lit set /lanes/store-bugs/rank '{above: /lanes/datetime}'
    /lanes/store-bugs/rank   5 → 2  (above /lanes/datetime)
    ⇒ /lanes/datetime/rank     2 → 3
    ⇒ /lanes/help/rank         3 → 4
    ⇒ /lanes/checkpoint/rank   4 → 5

### Rank one lane just below another
    was: lit rank links-scale-dy46.aof --below links-scale-dy46.48k
    $ lit set /lanes/scale-aof/rank '{below: /lanes/scale-48k}'
    /lanes/scale-aof/rank   7 → 4  (below /lanes/scale-48k)
    ⇒ /lanes/help/rank      4 → 5
    …

### Thirteen tickets that are one sequence, as one lane
The session's 13-call `rank --top` / `--below` chain was an ordered list.

    was: lit rank kg2.c44 --top && lit rank xu4.1il --below kg2.c44 && lit rank xu4.53j --below xu4.1il && … (13 calls)
    $ lit add /lanes/release '[kg2.c44, xu4.1il, xu4.53j, bue.hqi, bue.1e6, lm3.7iy, lm3.1tt, kg2.1tz, 7du.5uw, 7du.usq, cjt.3ns, b2q.8ew, b2q.3cn]' --dry-run
    + /lanes/release  [kg2.c44, xu4.1il, …, b2q.3cn]
    ⇒ xu4.1il/blocked_by = kg2.c44
    ⇒ xu4.53j/blocked_by = xu4.1il
    …
    ⇒ b2q.3cn/blocked_by = b2q.8ew
    (dry run: nothing written)

### Ten independent epics, ordered by priority
The ten-call variant ranked epics, which are not a sequence. Each epic's lane gets its own rank.

    was: lit rank kg2 --top; lit rank xu4 --below kg2; lit rank bue --below xu4; …
    $ lit set /lanes/kg2/rank top /lanes/xu4/rank '{below: /lanes/kg2}' /lanes/bue/rank '{below: /lanes/xu4}' /lanes/lm3/rank '{below: /lanes/bue}'
    /lanes/kg2/rank   6 → 1
    /lanes/xu4/rank   1 → 2
    /lanes/bue/rank   9 → 3
    /lanes/lm3/rank   2 → 4
    ⇒ /lanes/acr/rank   3 → 5
    …

### Read the whole backlog
    was: lit backlog | head -150
    $ lit get /lanes
    /lanes/release-pin    rank 1  pin        backlog                    1/1  urgent
    /lanes/store-bugs     rank 2  3k6u.9r8   backlog                    1/4
    /lanes/datetime       rank 3  b64        impl  this checkout     1/3
    /lanes/checkpoint     rank 4  3k6u.l1x   impl  stream ufijgjuy   1/3
    /lanes/7a2            rank 5  7a2        backlog                    1/1
    …

### Which top lanes are claimed
    was: lit backlog | grep -A2 -E '^\s*[12]\. ' | grep -E 'claimed'
    $ lit get '/lanes/*/claim'
    /lanes/datetime/claim     this checkout
    /lanes/checkpoint/claim   stream ufijgjuy  5 minutes ago

### An epic that holds two parallel lanes
`rin` has a parser stream and an evaluator stream. Neither waits on the other.

    $ lit add rin/lanes/parser '[rin.2ae, rin.k3x]'
    + rin/lanes/parser  [rin.2ae, rin.k3x]
    $ lit add rin/lanes/eval '[rin.9u9, rin.m0p, rin.t7c]'
    + rin/lanes/eval  [rin.9u9, rin.m0p, rin.t7c]
    $ lit get rin/lanes
    rin/lanes/parser  rank 6
      rin.2ae   review
      rin.k3x   backlog
    rin/lanes/eval  rank 7
      rin.9u9   backlog
      rin.m0p   backlog
      rin.t7c   backlog

### An urgent ticket as a one-ticket lane ranked top
Nothing had published since v1.156.4.

    was: lit new --type bug --topic release --top --title "Nothing has published since v1.156.4"
    $ lit add 792a/children '{title:"Nothing has published since v1.156.4" priority:urgent}'
    + 792a.pin
    $ lit add /lanes/release-pin pin
    + /lanes/release-pin  [pin]
    $ lit set /lanes/release-pin/rank top
    /lanes/release-pin/rank   12 → 1
    ⇒ /lanes/store-bugs/rank  1 → 2
    ⇒ /lanes/datetime/rank    2 → 3
    …

### What next serves when this checkout holds a lane
A higher lane whose head is not urgent does not pull it away.

    was: lit next
    $ lit get /next
    /next   b64   /lanes/datetime 1/3   held by this checkout

### What next serves when an urgent lane sits above the held one
    $ lit get /next
    /next   pin   /lanes/release-pin 1/1   urgent, ranked above /lanes/datetime (held by this checkout)

### What next serves when this checkout holds no lane
    $ lit get /next
    /next   3k6u.9r8   /lanes/store-bugs 1/4   top-ranked unclaimed lane

### Drop a lane claim without finishing
The checkout is leaving the lane mid-way. The only way to do that is a write that says so.

    $ lit rm /lanes/datetime/claim
    - /lanes/datetime/claim
    ⇒ /lanes/datetime   claim this checkout → (none)
    ⇒ /next   3k6u.9r8   /lanes/store-bugs

### Release a dead session's claim and put the ticket back
The plan phase had sat for ~12 hours with no commits or branch.

    was: lit open etg.bci --reason "Plan phase ~12h, no commits or branch; dead session's claim released"
    $ lit add etg.bci/comments "Plan phase ~12h, no commits or branch; dead session's claim released."
    + etg.bci/comments/r8v
    $ lit set etg.bci/status backlog
    etg.bci/status   plan → backlog
    ⇒ etg.bci/claim   claude_f335… → (none)

## Lanes at the edges

### Take one ticket out of a lane

    was: lit update tej.5 --lane ""
    $ lit rm /lanes/datetime/b64
    - /lanes/datetime/b64
    ⇒ b64/lane = b64
    ⇒ fw9/blocked_by = dee   (was b64)

### Dissolve a lane I hold

    $ lit rm /lanes/datetime
    - /lanes/datetime   [dee, b64, fw9]
    ⇒ dee/lane = dee
    ⇒ b64/lane = b64
    ⇒ fw9/lane = fw9
    ⇒ dee/claim = this checkout (its own lane now)

### Reorder a lane so the claimed ticket is no longer its head

    $ lit set /lanes/datetime '[b64, dee, fw9]'
    /lanes/datetime  [dee, b64, fw9] → [b64, dee, fw9]
    ⇒ b64/blocked_by = (none)
    ⇒ dee/blocked_by = b64

### The held lane's head waits on an edge from outside the lane

    $ lit get /lanes/datetime
    /lanes/datetime  rank 4
      dee   done
      b64   backlog   this checkout   blocked_by bryp (bryp/blocks)
      fw9   backlog

### An edge that closes a cycle through a lane

    $ lit add fw9/blocks dee
    error (reason=cycle): fw9/blocks dee closes a cycle: dee → b64 → fw9 (/lanes/datetime) → dee; nothing written

## A grooming pass, in order

### 1. Read every open ticket cheaply
Two lines per ticket, instead of the 270 KB the `show` loop printed.

    $ lit get '(status=backlog)/title' '(status=backlog)/lane'
    792a.b64/title   dateInZone accepts fixed offsets like +05:30
    792a.b64/lane    datetime
    rin.k3x/title    template with no pipeline passes nil dot
    rin.k3x/lane     parser
    …

### 2. Read the backlog in served order
    $ lit get /lanes
    /lanes/store-bugs   rank 2  3k6u.9r8   backlog                  1/4
    /lanes/datetime     rank 9  792a.dee   impl  this checkout   1/3
    …

### 3. Read the bodies of the tickets that look alike
    $ lit get 2d80.d66/body 2d80.c7e/body lq3.9od/body
    2d80.d66/body   The 'no tools' Unknown rule fires on …
    2d80.c7e/body   Unknown: no tools listed for …
    lq3.9od/body    The warning rules classify a turn with no tools …

### 4. Close a duplicate and move its acceptance criteria to the canonical ticket, in one call
The canonical body is the merged text on stdin. The duplicate closes against it.

    $ lit set lq3.9od/body - 2d80.d66/status dup 2d80.d66/of lq3.9od < /tmp/groom/9od-merged.md
    lq3.9od/body      v2 → v3  (+6 −0 lines)
    2d80.d66/status   backlog → dup
    2d80.d66/of       (none) → lq3.9od

### 5. Close a ticket whose work is already done
    was: lit close links-error-output-ckk7 --resolution obsolete --reason "groom: already satisfied - stderr now prints …"
    $ lit set ckk7/status obsolete ckk7/reason "groom: already satisfied; stderr prints the code and reason since #811"
    ckk7/status   backlog → obsolete
    ckk7/reason   (none) → groom: already satisfied; stderr prints the code and reason since #811

### 6. Rewrite a description the reader would misread
    $ lit set 9ym/body - <<'EOF'
    The statusline shows the lane's head and its position (2/3).
    Done when: a held lane renders as "datetime 2/3", a solo ticket as its id.
    EOF
    9ym/body   v4 → v5  (+2 −6 lines)

### 7. Put the trio that shares files into one lane
    $ lit add /lanes/datetime '[792a.dee, 792a.b64, 792a.fw9]'
    + /lanes/datetime  [792a.dee, 792a.b64, 792a.fw9]
    ⇒ 792a.b64/blocked_by = 792a.dee
    ⇒ 792a.fw9/blocked_by = 792a.b64

### 8. Rank the lanes
    $ lit set /lanes/datetime/rank '{above: /lanes/store-bugs}'
    /lanes/datetime/rank     9 → 2  (above /lanes/store-bugs)
    ⇒ /lanes/store-bugs/rank   2 → 3

### 9. Read the advisories
`/integrity` lists what the fold could not settle silently; nothing on it is a repair
lit runs. Each line names the write that clears it.

    was: lit doctor
    $ lit get /integrity
    /integrity   2 advisories
      /lanes/help/rank        placed at the bottom: its anchor /lanes/eval no longer exists   lit set /lanes/help/rank '{below: …}'
      jcj.l1e/blocks/ob5      ob5 is deleted                                                  lit rm jcj.l1e/blocks/ob5

### 10. Clear them, shown before it is done
    was: lit doctor --fix
    $ lit set /lanes/help/rank '{below: /lanes/scale-aof}' --dry-run
    /lanes/help/rank   12 → 6  (below /lanes/scale-aof)
    (dry run: nothing written)
    $ lit set /lanes/help/rank '{below: /lanes/scale-aof}'
    /lanes/help/rank   12 → 6  (below /lanes/scale-aof)
    $ lit rm jcj.l1e/blocks/ob5
    - jcj.l1e/blocks/ob5

### 11. Report what changed
The echoes above are the grooming report. A session that saved them has the whole diff.

    $ lit get /integrity
    /integrity   no advisories

## Undo

### Undo a title edit from its own echo

    $ lit set 9ym/title "Status line hides the ceiling when idle"
    9ym/title  "Status line shows the ceiling only past 80%" → "Status line hides the ceiling when idle"
    $ lit set 9ym/title "Status line shows the ceiling only past 80%"
    9ym/title  "Status line hides the ceiling when idle" → "Status line shows the ceiling only past 80%"

### Undo a description rewrite

    was: lit history yjx --version description@1 > /tmp/yjx-v1.txt; lit update yjx --description "$(cat /tmp/yjx-v1.txt)"
    $ lit get yjx/body/versions
    yjx/body/versions
      1  Brandon Fryslie  2026-10-19T16:02Z
      2  claude_5c44…     2026-11-02T21:10Z
      3  claude_5c44…     2026-11-02T21:31Z
    $ lit get yjx/body/versions/1 | lit set yjx/body -
    yjx/body  v3 → v4 (same text as v1)

### I meant to add a label and replaced the list

    $ lit set 9to/labels '[decompose]'
    9to/labels  [external, needs-design] → [decompose]
    $ lit set 9to/labels '[external, needs-design, decompose]'
    9to/labels  [decompose] → [external, needs-design, decompose]

### Find everything this session changed, to undo it

    $ lit get '(updated>-2h)/history/(by=claude_5c44)'
    yjx/history/e91   body v1 → v2
    yjx/history/e94   body v2 → v3
    9ym/history/e12   title "Status line shows…" → "Status line hides…"

## The model as data

The column graph, the requirements and the handlers live in `.lit/workspace.toml`. A
`set` on a model path edits the working-tree draft and says so; the draft is in force
once it is committed and applied with `lit config commit`.

### Which column graph governs this ticket, and where each column comes from

    was: lit workflows show rin.4ou   →   no workflow definition with id "rin.4ou"
    $ lit get rin.4ou/columns
    rin.4ou/columns  (resolved, nearest ancestor per column)
      backlog     initial next:[plan] exit:[ready]   /columns
      plan        next:[audit]                       rin/columns
      audit       next:[impl]                        rin/columns
      impl        next:[review] entry:[plan/verdict=pass]   /columns
      review      next:[merge]                       /columns
      merge       next:[done] entry:[pr/verdict=pass]       /columns
      done        terminal                           /columns
      dup         terminal entry:[of]                /columns
      superseded  terminal entry:[of]                /columns
      obsolete    terminal entry:[reason]            /columns
      wontfix     terminal entry:[reason]            /columns

### Add an audit column to one epic

    $ lit set rin/columns/audit '{next:[impl]}' rin/columns/plan/next '[audit]'
    rin/columns/audit      (none) → {next:[impl]}       draft: .lit/workspace.toml edited, not applied
    rin/columns/plan/next  [impl] (from /columns) → [audit]   draft
    $ git commit -am "rin audits before impl" && lit config commit
    applied version 7 (blob 3e1a9c0d), replaces version 6
    ⇒ 6 tickets under rin now resolve plan → audit → impl

### Add a requirement to a column

Nothing in the live-verification epic merges until it carries the `live-verified`
label. An `exit` requirement is checked on every forward transition out of the column.

    $ lit set mel/columns/review/exit '[{test:"labels=live-verified" severity:error}]'
    mel/columns/review/exit  (none) → [{test:"labels=live-verified" severity:error}]   draft
    $ git commit -am "mel: live-verified before merge" && lit config commit
    applied version 8 (blob 7e0f15c2), replaces version 7

### Refused by that requirement

    $ lit set mel.l1e/status merge
    error (reason=gate): mel.l1e/status review → merge refused by review/exit; nothing written
      block  labels=live-verified   mel.l1e/labels = [external]

### Read the guidance a ticket in a custom column will see

Guidance is a handler bound `in <column>`; it prints after the ticket.

    was: lit quickstart review
    $ lit get rin.4ou
    $ = links-hierarchy-rin.4ou
    $/status   audit
    …
    guidance (audit): Run the law audit on the diff before code review.

### Remove a column that tickets are in

The edit lands in the draft; the apply is what refuses, since a version may not drop
a column a ticket still holds.

    $ lit rm /columns/merge
    - /columns/merge   draft: .lit/workspace.toml edited, not applied
    $ git commit -am "drop merge" && lit config commit
    error (reason=invalid_value): version 9 removes columns.merge while 0814 s33 tbk hold it, and columns.review.next names it; nothing applied

### Remove an override nobody occupies

    $ lit rm rin/columns/audit rin/columns/plan/next --dry-run
    - rin/columns/audit
    - rin/columns/plan/next
    ⇒ rin/columns/plan/next = [impl] (from /columns)
    (dry run: nothing written)

### Add a terminal column that requires a field

    $ lit set /columns/superseded '{terminal:true entry:[of]}'
    /columns/superseded  (none) → {terminal:true entry:[of]}   draft

### Close as superseded without naming what supersedes it

    was: lit close 2ka --resolution superseded --of 6op --reason "…"
    $ lit set 2ka/status superseded
    error (reason=gate): 2ka/status backlog → superseded refused by superseded/entry; nothing written
      block  of   2ka/of = (none)
    $ lit set 2ka/status superseded 2ka/of 6op 2ka/reason "user decided: too-large results go whole to a file"
    2ka/status  backlog → superseded
    2ka/of      (none) → 6op
    2ka/reason  (none) → "user decided: too-large results go whole to a file"

### A transition that is not in next

    was: lit phase tbk --to merge   (from impl)
    $ lit set tbk/status merge
    error (reason=not_in_next): tbk/status impl → merge; impl/next = [review]; nothing written

### Park an in-progress ticket, or drop it as obsolete
A column no `next` names is entered from everywhere: `backlog`, and every terminal
column, are reached with no edge list. Parking into the initial column ends the hold.

    was: lit open 37o --reason "parked behind bryp"; lit close 2m0 --resolution obsolete --reason "…"
    $ lit set 37o/status backlog 37o/reason "parked behind bryp"
    37o/status  impl → backlog
    37o/reason  (none) → "parked behind bryp"
    ⇒ 37o/claim  this checkout → (none)
    $ lit set 2m0/status obsolete 2m0/reason "ws_client deleted in PR #55"
    2m0/status  review → obsolete
    2m0/reason  (none) → "ws_client deleted in PR #55"
    ⇒ 2m0/claim  stream ufijgjuy (elsewhere) → (none)

### One selector across two dialects of review

The live-verification epic defines its own `review`; everything else uses the root's.

    $ lit get '(status=review)/title'
    mel.l1e/title    Scene load reports a missing asset with its warning
    sjhy.ju8/title   Menus open on the focused display
    $ lit get mel/columns/review/next /columns/review/next
    mel/columns/review/next   [live-verify]
    /columns/review/next      [merge]

### Give one epic's claims a longer life

    $ lit set px1f/claim_ttl 12h
    px1f/claim_ttl  4h (from /) → 12h   draft: .lit/workspace.toml edited, not applied
    $ git commit -am "px1f: 12h claims" && lit config commit
    applied version 10 (blob c81e07a2), replaces version 9
    ⇒ /lanes/px1f claim expires 2026-11-03T22:04Z (was 14:04Z)

### Where does this claim's expiry come from?

Expiry is measured from the holder's newest event on any member of the lane, against
the `claim_ttl` resolved by nearest ancestor.

    $ lit get px1f.9u9/claim px1f.9u9/claim_ttl
    $ = promptctl-testing-px1f.9u9
    $/claim        stream voice-di1 (elsewhere)   newest event 2026-11-03T10:04Z
    $/claim_ttl    12h (from px1f)

## Derived fields

### Set a derived field

    $ lit set dee/claim claude_64e3
    error (reason=derived): dee/claim is derived, not settable (field table: claim  scalar  derived  the lane's holder, from its latest start or finish); nothing written

### Type the doc's own lane example

    $ lit set 'unfiled.*/lane' @id
    error (reason=derived): unfiled.*/lane is derived from lane membership, not settable; nothing written
    $ lit rm '/lanes/unfiled/*'
    - /lanes/unfiled/2ku
    - /lanes/unfiled/207
    - /lanes/unfiled/oq5
    ⇒ 2ku/lane = 2ku
    ⇒ 207/lane = 207
    ⇒ oq5/lane = oq5

### Clear a ticket's blocker by hand

    $ lit set b64/blocked_by ""
    error (reason=derived): b64/blocked_by is derived: dee precedes b64 in /lanes/datetime; reorder the lane or remove b64 from it; nothing written

### Blocked by lane position versus by an edge

    was: lit show 37o | grep -A3 '^blocked_by:'
    $ lit get b64/blocked_by 37o/blocked_by
    b64/blocked_by   dee    (position 2 in /lanes/datetime)
    37o/blocked_by   bryp   (bryp/blocks)

### Reopen a closed epic by reopening a child

    was: lit open o3mh   →   cannot open epic: it is closed, and an epic's state derives from its children
    $ lit set o3mh.avl/status backlog o3mh.avl/reason "Done-when fails on the second checkout"
    o3mh.avl/status  done → backlog
    o3mh.avl/reason  (none) → "Done-when fails on the second checkout"
    ⇒ o3mh/stage = active (was terminal)

### Rank a ticket

    was: lit rank aof --below 48k
    $ lit set aof/rank '{below: 48k}'
    error (reason=unknown_field): a ticket has no field rank; rank is on lanes. aof is in /lanes/scale: lit set /lanes/scale/rank '{below: <lane>}'

### Change a ticket's topic

`topic` is not a field; an area of focus is a label.

    was: lit update bwy.16 --topic context   →   unknown flag: --topic
    $ lit set bwy.16/topic context
    error (reason=unknown_field): a ticket has no field topic; for an area of focus: lit add bwy.16/labels context; nothing written

### Turn a leaf into a node

An item's level is where it sits in the tree, one below its parent, and nothing sets it.

    was: lit update hje --type epic   →   cannot change issue_type between container ([epic]) and leaf types
    $ lit set hje/level 3
    error (reason=derived): hje/level is its depth in the tree, not settable; nothing written

## Integrity

The fold settles every concurrent contradiction deterministically; `/integrity` lists
what it settled and what it could not apply, each with its writer, and the write that
would clear it. There is no repair verb.

### Is the store healthy?

    was: lit doctor
    $ lit get /integrity
    /integrity   4 advisories
      stale        last fetch 3h ago; the view may be behind the remote
      undeclared   lm3.7iy is in column audit, which no version its ancestors declare   lit set lm3.7iy/status plan
      anchor       /lanes/lm3 placed at the bottom: its anchor /lanes/device-sources no longer exists
      dropped      2 events on 3aj.8k1 from claude_c93e… arrived after its purge and were not applied

### Two concurrent edges that close a cycle

Each writer's edge was acyclic when written. The fold keeps the earlier in total order
and lists the later; `/next` reads neither as a blocker until someone chooses.

    $ lit get /integrity
    /integrity   1 advisory
      cycle   bryp/blocks/37o (claude_a1c0…) closes a cycle with 37o/blocks/bryp; not applied   lit rm 37o/blocks/bryp  or  lit rm bryp/blocks/37o

### Two writers fixed one segment offline

Each item keeps its id; the segment resolves to neither, and both are listed.

    $ lit get bwy.16/title
    error (reason=ambiguous): bwy.16 names 2 tickets; give more of the id
      bwy.16   "Context budget reads the model's window"   claude_a1c0…
      bwy.16   "Speech pipeline drops the first word"      claude_c93e…

## Delete, restore, purge

### Delete a ticket filed in error

`rm x` is a reversible tombstone: the events stay, the ticket leaves every listing.

    was: lit delete fko --reason "made in error: a doc edit, committed to master"
    $ lit add fko/comments "Deleted: made in error, a doc edit committed to master."
    + fko/comments/a210
    $ lit rm fko
    - fko   (tombstone; the events stay)

### Restore a deleted ticket

    was: lit restore 9ym --reason "deleted in error; not phase-related"
    $ lit set 9ym/deleted false
    9ym/deleted  true → false

### Read a deleted ticket

    $ lit get 9yn/status 9yn/deleted
    $ = houdini-tuneup-ef3.9yn
    $/status    done
    $/deleted   true
    $ lit get '(deleted=true text~"probe")/title'
    9yn/title   PROBE ticket (delete me)
    9ci/title   probe-delete-me

### Purge a probe ticket for good

    was: lit purge 8k1 --yes
    $ lit rm 3aj.8k1 --purge
    - 3aj.8k1   (comments=3 labels=0 edges=2 events=3)

### Purge an abandoned epic, shown first

    $ lit rm 3aj --purge --dry-run
    - 3aj
    - 3aj.8k1   (comments=3 edges=2)
    - 3aj.evr   (comments=1 edges=0)
    - 3aj.yjb   (comments=0 edges=1)
    ⇒ /lanes/monetization removed (no members left)
    (dry run: nothing written)

## Root paths

### What is at the root?

    $ lit get /
    /
      children    14 top-level items
      lanes       23
      columns     10
      claim_ttl   4h
      config      version 7 in force; .lit/workspace.toml matches
      integrity   no advisories
      next        b64

### The root column graph

    was: lit workflows
    $ lit get /columns
    /columns
      backlog     initial  next:[plan]  exit:[ready]
      plan        next:[impl]
      impl        next:[review]  entry:[plan/verdict=pass]
      review      next:[merge]   entry:[plan/verdict=pass (warning)]
      merge       next:[done]    entry:[pr/verdict=pass]
      done        terminal
      dup         terminal  entry:[of]
      superseded  terminal  entry:[of]
      obsolete    terminal  entry:[reason]
      wontfix     terminal  entry:[reason]

### Every lane's rank

    was: lit queue
    $ lit get '/lanes/*/rank'
    /lanes/c44/rank          1
    /lanes/store-bugs/rank   2
    /lanes/checkpoint/rank   3
    /lanes/datetime/rank     4

### A whole subtree, depth by depth

    was: lit tree   →   unknown command "tree"
    $ lit get 'rin.*/title' 'rin.*.*/title'
    $ = links-hierarchy-rin
    $.4ou/title       Ancestors resolve workflow by walking up
    $.4ou.1ab/title   A child's state reads its own subtree's dialect
    $.9m5/title       Lanes are cross-cutting

## Output shape corners

### `--json` on a glob

    $ lit get '792a.*/status' --json
    {"792a.dee/status":"done","792a.b64/status":"impl","792a.fw9/status":"backlog"}

### Two paths under one ancestor

    $ lit get 792a.dee/title 792a.dee/status 792a.b64/status
    $ = template-unfiled-792a
    $.dee/title    dateInZone accepts zone names in any letter case
    $.dee/status   done
    $.b64/status   impl

### Guidance printed after the data

    $ lit get dee
    $ = template-unfiled-792a.dee
    $/title    dateInZone accepts zone names in any letter case
    $/status   merge
    $/lane     datetime
    $/claim    this checkout
    guidance (merge): Once it has merged, close it: lit set $/status done. The echo names the next ticket in your lane.

### A ticket in a subtree with its own column

    $ lit get rin.4ou
    $ = links-hierarchy-rin.4ou
    $/title    Ancestors resolve workflow by walking up
    $/status   audit
    $/claim    this checkout
    guidance (audit): Run the law audit on the diff before code review.

### A multi-line value in path-tab-value output

    $ lit get '792a.*/body'
    792a.dee/body   "Go's LoadLocation is case-sensitive; dateInZone is not.\n\nDone when: …"
    792a.b64/body   "Builds on dee.\n\nDone when: …"

### A lane with a finished member

    $ lit get /lanes/datetime
    /lanes/datetime  rank 4
      dee   done
      b64   impl   this checkout
      fw9   backlog

### Read, edit, write without JSON

    $ lit get '(parent=z6sv priority=urgent)/priority' | awk '{print $1, "normal"}' | xargs lit set
    z6sv.dbc/priority  urgent → normal
    z6sv.463/priority  urgent → normal
    z6sv.bd3/priority  urgent → normal

### Commits and PRs for a ticket

    was: lit log 9uht   →   a2292fc4 2026-10-21 feat(daemon): a fed bar shows the pace…
    $ lit get 9uht/history
    9uht/history
      e01  2026-10-15T05:11Z  claude_214f…  status backlog → plan   ⇒ claim = claude_214f…
      e02  2026-10-15T06:02Z  claude_214f…  plan/revisions + 1
      e07  2026-10-21T19:40Z  claude_214f…  status merge → done

## Odd nodes and refusals

### Remove a label that is not there

    was: lit label rm 9to external
    $ lit rm 9to/labels/external
    error (reason=not_found): 9to/labels has no external; 9to/labels = [needs-design, decompose]; nothing written

### Remove a comment twice

    $ lit rm 03v/comments/2f27
    - 03v/comments/2f27
    $ lit rm 03v/comments/2f27
    error (reason=not_found): 03v/comments has no 2f27; nothing written

### Append to a scalar

    $ lit add dee/title "in any letter case"
    error (reason=not_a_list): dee/title is a scalar; add appends to a list; to replace it: lit set dee/title "…"; nothing written

### Empty a list

    $ lit set 9to/labels '[]'
    9to/labels  [needs-design, decompose] → []

### Set with an odd number of arguments

    $ lit set dee/status done b64/status
    error (reason=usage): set takes path value pairs; b64/status has no value; nothing written

### A field that does not exist

    was: lit show jsh --field comments   →   unknown --field "comments"; valid fields: assignee, created_at, …
    $ lit get dee/phase
    error (reason=unknown_field): a ticket has no field phase; phases are columns: lit get dee/status
      ticket fields: blocked_by blockers blocks body checklist claim claim_ttl columns comments
                     created_at held history labels lane of parent plan pr priority prompt reason
                     stage status tier title updated_at

### A glob that matches nothing

    $ lit set 'ogn.*/priority' urgent
    error (reason=no_match): ogn.* matched no tickets (ogn has no children); nothing written

### A selector with an unknown key

    was: lit ls --ids … --columns id,status   →   unknown --columns name "status"; valid columns: … state …
    $ lit get '(state=open)/title'
    error (reason=unknown_field): selector key state is not a field; did you mean status; nothing printed

### A selector value no column graph defines

    was: lit ls --status closed,open,in_progress,blocked --search b2q.   (four times, unchanged)
    $ lit get '(status=blocked)/title'
    error (reason=invalid_value): no resolved column graph has a column blocked
      columns: backlog plan impl review merge done dup obsolete superseded wontfix; audit under rin; live-verify under mel
      blocked is the derived field blocked_by
    nothing printed

### Stdin twice in one command

    $ lit set 8ro6/body - 8ro6/prompt - < /tmp/8ro6/both.md
    error (reason=usage): - reads stdin once per command; it appears twice (8ro6/body, 8ro6/prompt); nothing written

### A `.` where a `/` belongs

    $ lit get 792a.dee.title
    error (reason=not_found): no ticket 792a.dee.title: 792a.dee has no child title; for the field: lit get 792a.dee/title

### A short id that names two tickets

    was: lit show lm3.1tt   →   issue "lm3.1tt" not found
    $ lit get 1tt
    error (reason=ambiguous): 1tt names 2 tickets: orange-iphone-watch-lm3.1tt, promptctl-voice-i15t.1tt; give more of the id

### Selector, glob, or literal left unquoted in zsh

    $ lit get (status=review)/title
    zsh: no matches found: (status=review)/title
    $ lit get unfiled.*/title
    zsh: no matches found: unfiled.*/title
    $ lit add a1h/plan/reviews {of:4 how:"/plan-review" result:"one-source-of-truth, single-enforcer: …" verdict:pass}
    zsh: parse error near `}'

### A title with a colon and quotes in it

    was: lit import --path /tmp/cb-breakdown/vocab.yaml   →   yaml: line 2: mapping values are not allowed in this context
    $ lit add unfiled/children '{title:daemon.log is mostly unknown-field lines}'
    error (reason=parse): value {…}: "is" is not k:v; quote a value that holds spaces; nothing written
    $ lit add unfiled/children - <<'EOF'
    {title:"daemon.log is ~90% 'schema: unknown field' lines"}
    EOF
    + unfiled.k4e
    ⇒ /lanes/k4e = [k4e]   rank 24

### A write where one path of three is refused

    $ lit set 2ku/status done 207/status done oq5/status done
    error (reason=not_in_next): oq5/status impl → done; /columns/impl/next = [review]
      2ku/status  merge → done   not written
      207/status  merge → done   not written
    nothing written

### Set a field to the value it already has

    was: lit done cjt   →   state_already_holds
    $ lit set dee/status impl
    dee/status  impl (unchanged)

## The old fumbles, in the new grammar

### Comment text given positionally

The value is positional, so the old failing form is now the working one.

    was: lit comment add links-supply-chain-w6m9.2 "PR #333 opened: …"
    $ lit add w6m9.2/comments "PR #333 opened: https://github.com/promptctl/links-issue-tracker/pull/333 — CycloneDX 1.6 SBOM …"
    + w6m9.2/comments/x4c

### Comment with no subcommand, or with -m or --message

    was: lit comment orange-widgets-5jo.nlf "From PR #49 review: …"
    was: lit comment add brandon-daemon-lifecycle-gad.3 -m "Completed by PR #163 …"
    was: lit comment add room-live-verify-56w --message "LIVE-VERIFIED …"
    $ lit add gad.3/comments "Completed by PR #163 …"
    + gad.3/comments/e9k

### Comment removal given the ticket id and the comment id

The old `comment rm` took only the comment id; the new path needs both.

    was: lit comment rm houdini-laws-etg.4id cmt-5ae7b8da-29f3-4328-bebf-6bb988c5e60b
    $ lit rm 4id/comments/5ae
    - 4id/comments/5ae

### Editing a comment

    was: lit comment edit --help
    $ lit set 3h8/comments/03d "The credential file is src/oauth.ts."
    3h8/comments/03d  "The credential file is src/login.ts." → "The credential file is src/oauth.ts."

### Several ids handed to show

    was: lit show orange-devices-bue.1e6 orange-devices-bue.chz orange-devices-bue.hqi
    $ lit get 1e6/status chz/status hqi/status
    1e6/status   backlog
    chz/status   impl
    hqi/status   done

### Comments asked for as a field

    was: lit show orange-iphone-watch-lm3.7iy --field status,labels,description,comments
    $ lit get 7iy/status 7iy/labels 7iy/body 7iy/comments
    $ = orange-iphone-watch-lm3.7iy
    $/status         plan
    $/labels         [watch]
    $/body           The watch app reads heart rate through …
    $/comments/2fa   [claude_96d7… Oct 24] Plan v5 drops the polling loop …

### JSON wanted for one field

    was: lit show promptctl-brain-vu2t.abp --json 2>/dev/null | jq -r '.plan // .issue.plan // empty' > /tmp/abp_break/plan_v4.md
    $ lit get abp/plan > /tmp/abp_break/plan_v4.md

### JSON wanted for a few fields

    was: lit show links-scale-dy46.19m --json
    $ lit get 19m/status 19m/plan/verdict --json
    {"19m/status":"impl","19m/plan/verdict":"pass"}

### Wrong id prefix

    was: lit show orange-drink-lm3.8pk
    $ lit get lm3.8pk/title
    Pairing survives the watch rebooting

### Filter by a field there was no flag for

    was: lit ls --topic devices
    $ lit get '(labels=devices)/title'
    orange-devices-bue.1e6/title   The device list shows a sweep in progress
    orange-devices-bue.k7r/title   A connected device sweeps with an empty name

### A selector key that is not a field

    was: lit ls --epic orange-alerts-cjt
    $ lit get '(epic=cjt)/title'
    error (reason=unknown_field): selector key epic is not a field; did you mean parent; nothing printed

### An epic's tickets, asked for with --epic

    was: lit ls --epic orange-alerts-cjt
    $ lit get '(parent=cjt)/status'
    orange-alerts-cjt.3ns/status   done
    orange-alerts-cjt.7pq/status   impl
    orange-alerts-cjt.v2a/status   backlog

### Next, scoped to one epic

    was: lit next --epic orange-admin-helper-b2q
    $ lit get b2q/lanes
    b2q/lanes
      /lanes/admin-helper   3cn   backlog

### A reason given to a dependency

    was: lit dep add candybar-testing-ihvy.cbz --blocked-by candybar-testing-0x6u.snf --reason "the gauge test needs the fake clock"
    $ lit add snf/blocks cbz
    + snf/blocks/cbz
    ⇒ cbz/blocked_by  [] → [candybar-testing-0x6u.snf]
    $ lit add cbz/comments "Waits on 0x6u.snf: the gauge test needs the fake clock."
    + cbz/comments/u7y

### Done on an epic whose children are all closed

    was: lit done orange-alerts-cjt
    $ lit set cjt/status done
    error (reason=unknown_field): cjt is a node and has no status; its stage derives from cjt.*/status; nothing written

### Phase given positionally

    was: lit phase brandon-unfiled-3248.8ro6 impl
    $ lit set 8ro6/status impl
    8ro6/status  plan → impl

### Status changed through update

    was: lit update brandon-menus-sjhy.ju8 --status open
    $ lit set ju8/status backlog
    ju8/status  plan → backlog
    ⇒ ju8/claim  this checkout → (none)

### A resolution that is not a state

    was: lit close houdini-install-aby.80r --resolution fixed
    $ lit set 80r/status fixed
    error (reason=invalid_value): no resolved column graph has a column fixed; terminal columns: done dup superseded obsolete wontfix; nothing written

### A priority level that does not exist

    was: lit update houdini-connections-ob5 --priority high
    $ lit set ob5/priority high
    error (reason=invalid_value): ob5/priority takes normal or urgent; got high; nothing written

### Reasons passed to the verdict

    was: lit verdict mock-mock-flu.17w fail --plan 0b7ecac89fe6… --law single-enforcer --body "…"
    $ lit add 17w/plan/reviews '{of:2 how:"/plan-review" result:"single-enforcer: …" verdict:fail}'
    + 17w/plan/reviews/t8b
    ⇒ 17w/plan/verdict = fail

### Guessed verbs: search, list, create, comments, epics

    was: lit search RacingFirstOpens
    $ lit get '(text~"RacingFirstOpens")/title'
    links-perf-kw6z.5/title   RacingFirstOpens opens the store once per race
    was: lit list
    $ lit get /lanes
    …
    was: lit create --type epic --title E1 --topic t1
    $ lit add /children '{title:E1}'
    + links-v3p
    was: lit comments candybar-isolated-daemon-bfab.agt
    $ lit get agt/comments
    …
    was: lit epics
    $ lit get /children
    …

## The admin boundary

### Publish the backlog

`init`, `sync`, `backup`, `upgrade` and the rest of the maintenance surface live under
`lit admin`, so the four-verb table stays legible.

    was: lit sync push
    $ lit sync push
    error (reason=unknown_verb): lit has four verbs: get set add rm; sync is lit admin sync
    $ lit admin sync
    sealed 1 segment, published; fetched 2 writers, verified

### Write while a sync is running

A command appends to this checkout's log and touches nothing else; sync seals and
publishes. Neither waits on the other, and there is no lock.

    was: lit verdict rm9w … --law single-enforcer   →   workspace_busy: held by pid 95859 (lit sync push --remote origin)
    $ lit add rm9w/plan/reviews '{of:3 how:"/plan-review" result:"single-enforcer: …" verdict:pass}'
    + rm9w/plan/reviews/k0d
    ⇒ rm9w/plan/verdict = pass

### Start a workspace

`admin init` writes the default profile to `.lit/workspace.toml` and applies the same
bytes as version one; the prefix is a line in that file.

    was: lit init   →   issue prefix refused: repository name "ws" yields none … run lit init --prefix <prefix>
    $ lit get /
    error (reason=not_initialized): no lit workspace in this repository; lit admin init
    $ lit admin init
    initialized  .lit/workspace.toml written (default profile) and applied as version 1; prefix ws
    recovery phrase: …

## What the examples could not say

Writing the examples above against the design found places the grammar has no
spelling. Each was tried, by a session in the transcripts or by a writer here, and
written as far as the grammar goes. The design doc already absorbed the ones that
were contradictions inside it or conventions every writer reached independently
(quoting, `stage`, the columns no `next` names, `/lanes/<id>`, all-or-nothing
writes, the refusal and dry-run shapes, `--json` keyed by path, short ids, `rm x` as
a reversible tombstone, `lit admin init`). What remains is open.

**Selectors and globs**

- No OR on one key (`status=backlog,impl`), no `>` or absolute dates
  (`updated>2026-11-02`), no existence or count predicate (`has comments`), no sort,
  no limit. Sessions used every one of these through `ls --query` and `grep -v`.
- Which fields `text~` searches; whether `labels=focus` means the list contains it;
  whether a selector matches terminal and deleted tickets by default. The examples
  assume every column, and deleted only when named.
- No selector names a level, so "every epic" has no spelling beyond `get /children`.
- Nothing selects below the ticket: comments by author, history events by writer.
  `rm '*/comments/*'` "filtered by author" in the design has no spelling.
- No recursive glob, no alternation, no multi-field path, no way to name an arbitrary
  set of ids. N ids × M fields is N×M paths, and one value on three unrelated tickets
  is three calls.
- A glob or selector cannot be a value: `add /lanes/policy 'e5r4.*'`.

**Values**

- No file form, and `-` reads stdin once per command, so N long bodies in one call
  need shell substitution, which the worktree guard refuses.
- Quoted strings define no escapes, and `path<TAB>value` output defines no encoding
  for a multi-line value, so the round trip the Output section promises is undefined
  for bodies.
- `add` has no many-pairs form. Eleven different grooming notes on eleven tickets is
  eleven calls, where `set` would take them as pairs.
- Whether an object may nest `children`, and whether items in one `add` can refer to
  each other. A `blocks` edge between two tickets minted by the same call needs a
  second call.

**Reasons**

- No reason on a write, an edge, or an `rm`. The old `--reason` on `update`, `dep
  add`, `delete`, and `archive` has nowhere to go but a comment written first.
  Sessions tried `dep add --reason` seven times in a row and dropped the reason.

**Plans and gates**

- A review names the revision it judged, `of`, and lands whether or not that revision
  is still the latest. Only the derived verdict ignores a review of an older revision,
  so a reviewer who read revision 21 while the author appended 22 passes nothing
  unread, and the echo says which revision is latest.
- "The reviewer is not the plan's author" compares two paths; the gate form shows
  only `path=literal`.
- Appending a revision to the plan of a ticket already in `impl` empties the verdict
  `impl` was entered on. What happens to the ticket, and what the echo says.
- The echo on a review cannot say whether the requirement now passes.
- `/next` exists only at the root. `b2q/next`, by nearest-ancestor, is undefined.

**Claims and lanes**

- "Claim expired" has no selector key; an expired claim prints as none.
- A reorder that puts a held, in-flight ticket behind a backlog one.
- Taking over work left mid-flight: a hold is established only by a start or a finish,
  so a checkout that releases a lane and moves its head `impl → review` holds nothing
  until it finishes a member, and `/next` serves that head to anyone.
- `get /me` prints the writer; "this checkout" in an echo is the only other place it
  shows.

**Fields**

- `follows_up`: the `followup` mapping drops the link. A comment's body: settable or
  append-only; sessions appended corrections on purpose.
- Commits and PRs that name a ticket are not nodes; `lit log` has no path.
- Two writers who fixed one segment: both are listed, each keeps its id, and how one
  of them is then addressed on its own has no spelling.
- Restoring a tombstoned ticket: `rm x` is reversible by design, and the verb that
  reverses it has no spelling here; the examples write `set x/deleted false`.
