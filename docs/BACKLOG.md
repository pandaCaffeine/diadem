# Backlog

Deliberately deferred ideas, with the reason and the trigger that would
revive them.

## DIDL-Lite metadata polish

Advertise richer `<res>` attributes in ContentDirectory Browse responses:

- `duration="H:MM:SS"` and `bitrate` — lets clients show a seek bar and
  estimate seek targets before opening the file;
- a real DLNA profile in the fourth protocolInfo field instead of `*`, e.g.
  `http-get:*:video/x-matroska:DLNA.ORG_PN=MATROSKA;DLNA.ORG_OP=01;DLNA.ORG_CI=0`.

Deferred (2026-11-14): VLC — the primary client — ignores both. It reads
duration from the file itself, and its seeking problems turned out to be
container physics (see below), so richer metadata would not have helped.
Revive if a built-in TV player misbehaves. Cost: either parse mp4/mkv
headers in-process (~150 lines) or probe via optional ffprobe — both were
v1 non-goals.

## Resolved: slow seeking on TV (2026-11-14) — don't re-investigate

Symptom: a +30s seek into the intro of 1080p BluRay rips buffered for
seconds in VLC on the TV; any seek past the intro was instant; the laptop
never showed the problem.

Root cause (proven, not guessed): sparse keyframes in high-bitrate intros.
Example: a 7.05s stretch of the intro with no keyframe at ~46 Mbps forms a
single 40 MB MKV cluster; Cues point to cluster starts, so any player that
lands via a cue must read from the cluster start forward to the target.

Ruled out along the way: HTTP layer (Content-Length, Content-Range,
Accept-Ranges, suffix ranges, HEAD — all verified textbook), missing Cues
indexes (files carry ~1 cue per second), remuxing with smaller clusters
(ffmpeg `-cluster_time_limit` cannot split keyframe-less spans — tested).
Not fixable server-side. Documented in README → Troubleshooting.
