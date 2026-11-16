# Backlog

Deliberately deferred ideas, with the reason and the trigger that would
revive them.

## DLNA profile in protocolInfo

Advertise a real DLNA profile in the fourth protocolInfo field instead of
`*`, e.g.
`http-get:*:video/x-matroska:DLNA.ORG_PN=MATROSKA;DLNA.ORG_OP=01;DLNA.ORG_CI=0`.

Deferred (2026-11-14): VLC — the primary client — ignores it, and mapping
each container/codec pair to a profile needs care. Revive if a built-in
TV player requires it.

Note (2026-11-16): the other half of the old "DIDL-Lite metadata polish"
entry — `res@duration`/`res@bitrate`/`dc:title` — shipped: the TUI
duration column made the container parsers worth having (ADR-0003), and
the attributes ride along.

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
