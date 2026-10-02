#!/usr/bin/env python3
"""SPIKE: worst-case table oracle inputs (version 2 text tables).

gen_tables.py <bits> <events> <out> [scrambled]

Every state of 2^bits is valid; event e sets bit e; the declared pairs are
(i, i+1 mod events). That machine is convergent. With "scrambled", state ids
are relabelled by a fixed pseudo-random permutation that keeps 0 at 0, so the
same machine is checked but lookups have no locality.
"""
import random
import sys

bits, events, out = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
scrambled = len(sys.argv) > 4 and sys.argv[4] == "scrambled"
n = 1 << bits
perm = list(range(n))
if scrambled:
    rest = perm[1:]
    random.Random(20261002).shuffle(rest)
    perm = [0] + rest
inv = [0] * n
for s, p in enumerate(perm):
    inv[p] = s
with open(out, "w") as f:
    f.write("gsm-tables 2\n%d %d\n" % (n, events))
    f.write("nf " + " ".join(map(str, range(n))) + "\n")
    pairs = []
    for i in range(events):
        pairs += [i, (i + 1) % events]
    f.write("pairs %d %s\n" % (events, " ".join(map(str, pairs))))
    for e in range(events):
        b = 1 << e
        f.write(" ".join(str(perm[inv[t] | b]) for t in range(n)) + "\n")
