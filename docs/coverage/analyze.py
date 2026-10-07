import itertools, collections, sys
import os
HERE=os.path.dirname(os.path.abspath(__file__))
exec(open(os.path.join(HERE,'catalog.py')).read())
N=len(R)
EXT=["PARAM","DIFF","LIN","AGG","HIST","SET","XREL","TIME","COMB","BITS"]
def alts(r):
    if r['scale']=='yes' or r['moves'] in ('','none'): return []
    return [frozenset(a.split('+')) for a in r['moves'].split('|')]
for r in R:
    for a in alts(r):
        assert a <= set(EXT), (r['id'],a)
    assert r['expr'] in ('comb','closure','enc','no'), r['id']
    assert r['scale'] in ('yes','toy','no'), r['id']
    if r['scale']=='yes': assert r['pr'] in ('collection','Build','Abstract','per-component','federation','CheckMigration'), r['id']
    else: assert r['pr'] in ('ARITH','AGG','TIME','REL','CLOS','XITEM','OTHER'), r['id']
    if r['expr']=='no': assert r['scale']=='no'
ids=[r['id'] for r in R]; assert len(set(ids))==N
pct=lambda a,b: f"{100*a/b:.0f}%"
c=collections.Counter(r['expr'] for r in R)
print("N",N, "expr",dict(c), "expressible", N-c['no'], pct(N-c['no'],N))
s=collections.Counter(r['scale'] for r in R)
print("scale",dict(s), pct(s['yes'],N))
print("paths",collections.Counter(r['pr'] for r in R if r['scale']=='yes'))
print("reasons",collections.Counter(r['pr'] for r in R if r['scale']!='yes'))
print("reason x scale",collections.Counter((r['pr'],r['scale']) for r in R if r['scale']!='yes'))
doms=[]
for r in R:
    if r['dom'] not in doms: doms.append(r['dom'])
for d in doms:
    rs=[r for r in R if r['dom']==d]
    print(d, len(rs), sum(r['expr']!='no' for r in rs), sum(r['scale']=='yes' for r in rs), sum(r['scale']=='toy' for r in rs), sum(r['scale']=='no' for r in rs))
kinds=collections.OrderedDict()
for r in R: kinds.setdefault(r['kind'],[]).append(r)
for k,rs in kinds.items():
    print(k, len(rs), sum(r['scale']=='yes' for r in rs), sum(r['scale']=='toy' for r in rs), sum(r['scale']=='no' for r in rs))
def covered(S):
    return [r['id'] for r in R if any(a<=S for a in alts(r))]
print("--- singles")
for x in EXT:
    part=[r['id'] for r in R if any(x in a for a in alts(r))]
    print(x, len(covered(frozenset([x]))), "part-of-route", len(part))
print("--- best pairs")
pairs=sorted(((len(covered(frozenset(p))),p) for p in itertools.combinations(EXT,2)), reverse=True)[:8]
for n,p in pairs: print(n,p)
print("--- best triples")
tr=sorted(((len(covered(frozenset(p))),p) for p in itertools.combinations(EXT,3)), reverse=True)[:6]
for n,p in tr: print(n,p)
print("--- greedy")
S=set(); 
for i in range(6):
    best=max((x for x in EXT if x not in S), key=lambda x: len(covered(frozenset(S|{x}))))
    S.add(best); print(i+1,best,len(covered(frozenset(S))))
print("none", [r['id'] for r in R if r['moves']=='none'])
print("all ext covered", len(covered(frozenset(EXT))))
