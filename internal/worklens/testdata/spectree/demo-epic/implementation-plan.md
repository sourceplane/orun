# Implementation Plan

## D0 — Lay the substrate
**Goal:** the two tables exist and replay.
- some bullet detail.

**Deps:** none (greenfield). **Done when:** dropping caches and replaying
reproduces reads; an event without an actor is rejected.

## D1 — Wire the surface
**Goal:** the read surface renders evidence.

**Deps:** D0. **Done when:** the list view shows rungs with evidence.
