# AGENTS.md

Diadem — console DLNA (UPnP AV) media server in Go. Domain terms live in
CONTEXT.md (root); plans, decisions and history in docs/PLAN.md and
docs/adr/.

## Go style

- Pass one string per `WriteString` call: `b.WriteString(a);
  b.WriteString(b)`, never `b.WriteString(a + b)`. Runtime concatenation
  allocates a throwaway string the builder then copies again; gopls calls
  it "Inefficient string concatenation" and the repo treats it as an
  error. Constant-only concatenation (`"lit" + "\n"`) is compile-time
  folded — leave it as is.

## Language

- Code comments, log lines and UI strings are English. Russian in this
  repo appears only in CONTEXT.md, docs/PLAN.md and docs/adr/.
- UI localization is a distant plan, not a reason to mix languages today.

## Git

- The user reviews changes and commits; agents leave the working tree
  uncommitted.
