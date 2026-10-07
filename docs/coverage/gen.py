import itertools, collections, re
import os
HERE=os.path.dirname(os.path.abspath(__file__))
exec(open(os.path.join(HERE,'catalog.py')).read())
N=len(R)
EXT=["LIN","AGG","XREL","SET","TIME","DIFF","BITS","COMB","HIST"]  # PARAM is implemented
OTHER_SUB={"ORD-11":"other: liveness","BKG-13":"other: liveness","PAY-12":"other: needs coordination",
 "BKG-11":"other: needs coordination","MSV-06":"other: needs coordination","MSV-12":"other: needs coordination",
 "MSV-05":"other: transport","MSV-14":"other: reductions do not combine"}
REASON={"ARITH":"arithmetic over wide ranges","AGG":"aggregate across items","TIME":"history or time",
 "REL":"unbounded or relational data","CLOS":"closure prevents reductions","XITEM":"cross-item constraint","OTHER":"other"}
EXPR={"comb":"combinator","closure":"closure","enc":"encoding","no":"no"}
VAL={k:k.lower() for k in ["V1","V2","V3","V4","V5","V6","V7","V8","V9","V10","T1","T2","T3","T4","T5","T6","N1","N2","N3"]}
def alts(r):
    if r['scale']=='yes' or r['moves'] in ('','none'): return []
    # PARAM is implemented: a set that needs it needs only the rest.
    return [frozenset(a.split('+'))-{"PARAM"} for a in r['moves'].split('|')]
def covered(S): return [r['id'] for r in R if any(a<=S for a in alts(r))]
pct=lambda a,b: f"{round(100*a/b)}%"
out={}
c=collections.Counter(r['expr'] for r in R); s=collections.Counter(r['scale'] for r in R)
expressible=N-c['no']
out['N']=str(N); out['EXPR_N']=str(expressible); out['EXPR_P']=pct(expressible,N)
out['YES_N']=str(s['yes']); out['YES_P']=pct(s['yes'],N); out['TOY_N']=str(s['toy']); out['TOY_P']=pct(s['toy'],N)
out['EXPRNO_N']=str(c['no']); out['NO_N']=str(s['no']); out['NO_P']=pct(s['no'],N)
hard=[r for r in R if r['kind'] not in ('lifecycle','guarantee')]
out['HARD_N']=str(len(hard)); out['HARD_YES']=str(sum(r['scale']=='yes' for r in hard)); out['HARD_P']=pct(sum(r['scale']=='yes' for r in hard),len(hard))
out['CEIL_N']=str(s['yes']+len(covered(frozenset(EXT)))); out['CEIL_P']=pct(s['yes']+len(covered(frozenset(EXT))),N)
out['NONE_N']=str(sum(1 for r in R if r['moves']=='none' and r['scale']!='yes'))
out['SUMMARY']="\n".join([
"| Measure | Count | Share |","|---|---:|---:|",
f"| Invariants catalogued | {N} | |",
f"| Expressible in gsm today (combinator, closure or encoding) | {expressible} | {pct(expressible,N)} |",
f"| ... directly with combinators | {c['comb']} | {pct(c['comb'],N)} |",
f"| ... with a closure | {c['closure']} | {pct(c['closure'],N)} |",
f"| ... through an encoding | {c['enc']} | {pct(c['enc'],N)} |",
f"| Not expressible | {c['no']} | {pct(c['no'],N)} |",
f"| **Verifiable at production scale today** | **{s['yes']}** | **{pct(s['yes'],N)}** |",
f"| Verifiable only at toy scale | {s['toy']} | {pct(s['toy'],N)} |",
f"| Not verifiable at any scale | {s['no']} | {pct(s['no'],N)} |",
f"| Verifiable at scale, excluding lifecycle rules and delivery or deadline guarantees | {out['HARD_YES']} of {len(hard)} | {out['HARD_P']} |",
])
paths=collections.Counter(r['pr'] for r in R if r['scale']=='yes')
out['PATHS']="\n".join(["| Path | Invariants |","|---|---:|"]+[f"| {p} | {n} |" for p,n in paths.most_common()])
# by kind
KORDER=["lifecycle","ordering","referential","uniqueness","temporal","aggregate","arithmetic","cross-item","guarantee"]
KDESC={"lifecycle":"Lifecycle and status rules","ordering":"Ordering (\"only moves forward\")","referential":"Referential and cross-service",
 "uniqueness":"Uniqueness","temporal":"Temporal (expiry, windows, \"never twice\", precedence)","aggregate":"Totals, counts and aggregates",
 "arithmetic":"Balances and amounts with arithmetic","cross-item":"Cross-item (double booking, overlap)","guarantee":"Delivery, deadline and no-revision guarantees"}
rows=["| Kind | Invariants | Yes at scale | Toy only | No |","|---|---:|---:|---:|---:|"]
for k in KORDER:
    rs=[r for r in R if r['kind']==k]
    rows.append(f"| {KDESC[k]} | {len(rs)} | {sum(r['scale']=='yes' for r in rs)} | {sum(r['scale']=='toy' for r in rs)} | {sum(r['scale']=='no' for r in rs)} |")
out['BYKIND']="\n".join(rows)
# by reason
nonyes=[r for r in R if r['scale']!='yes']
rc=collections.Counter(r['pr'] for r in nonyes)
rows=["| Blocking reason | Invariants | Share of the blocked |","|---|---:|---:|"]
for k in ["ARITH","TIME","XITEM","AGG","REL","CLOS","OTHER"]:
    n=rc.get(k,0)
    label=REASON[k]
    if k=="OTHER":
        sub=collections.Counter(OTHER_SUB[r['id']].split(': ')[1] for r in nonyes if r['pr']=='OTHER')
        label+=" ("+", ".join(f"{a} {b}" for a,b in sub.most_common())+")"
    rows.append(f"| {label} | {n} | {pct(n,len(nonyes))} |")
rows.append(f"| **Total blocked** | **{len(nonyes)}** | |")
out['BYREASON']="\n".join(rows)
# by domain
doms=[]
for r in R:
    if r['dom'] not in doms: doms.append(r['dom'])
rows=["| Domain | Invariants | Expressible | Yes at scale | Toy only | No | Main blocker |","|---|---:|---:|---:|---:|---:|---|"]
for d in doms:
    rs=[r for r in R if r['dom']==d]
    bl=collections.Counter(r['pr'] for r in rs if r['scale']!='yes').most_common()
    top=", ".join(f"{REASON[a] if a!='OTHER' else 'other'} ({b})" for a,b in bl[:2])
    rows.append(f"| {d} | {len(rs)} | {sum(r['expr']!='no' for r in rs)} | {sum(r['scale']=='yes' for r in rs)} ({pct(sum(r['scale']=='yes' for r in rs),len(rs))}) | {sum(r['scale']=='toy' for r in rs)} | {sum(r['scale']=='no' for r in rs)} | {top} |")
out['BYDOMAIN']="\n".join(rows)
# extension table
gre=[]; S=set(); prev=0
for i in range(len(EXT)):
    cand=[x for x in EXT if x not in S]
    best=max(cand, key=lambda x:(len(covered(frozenset(S|{x}))), -EXT.index(x)))
    S.add(best); n=len(covered(frozenset(S))); gre.append((best,n-prev,n)); prev=n
greedy={x:(g,n,i+1) for i,(x,g,n) in enumerate(gre)}
alone={x:len(covered(frozenset([x]))) for x in EXT}
part={x:sum(1 for r in R if any(x in a for a in alts(r))) for x in EXT}
withp={x:len(covered(frozenset([x]))) for x in EXT}
out['GREEDY']="\n".join(["| Step | Add | Invariants moved by this step | Cumulative yes at scale |","|---:|---|---:|---:|"]+
  [f"| {i+1} | {x} | +{g} | {s['yes']+n} ({pct(s['yes']+n,N)}) |" for i,(x,g,n) in enumerate(gre) if g>0])
NAME={"PARAM":"Event parameters","LIN":"General linear arithmetic","AGG":"Aggregate reduction (counter abstraction)",
 "XREL":"Cross-item relational constraints","SET":"Set and uniqueness constraints","TIME":"Time and expiry encodings",
 "DIFF":"Difference-constraint arithmetic","BITS":"A larger enumeration limit","COMB":"Combining reductions","HIST":"History encoding helpers"}
DIFFI={"PARAM":"Low (in the mechanized model already)","LIN":"Medium (theorem exists; solver and saturation open)","AGG":"High (new theorem)",
 "XREL":"High (new theorem)","SET":"Medium to high","TIME":"Medium (PARAM plus DIFF)","DIFF":"Medium","BITS":"None (engineering)",
 "COMB":"Low to medium","HIST":"None (sugar)"}
rows=["| Rank | Extension | Alone | On some route | Greedy gain | Theory difficulty |","|---:|---|---:|---:|---:|---|"]
order=sorted(EXT,key=lambda x:(greedy[x][2]))
for i,x in enumerate(order):
    rows.append(f"| {i+1} | {NAME[x]} (`{x}`) | {alone[x]} | {part[x]} | +{greedy[x][0]} | {DIFFI[x]} |")
out['EXTTABLE']="\n".join(rows)
for x in EXT:
    out['ALONE_'+x]=str(alone[x]); out['PART_'+x]=str(part[x]); out['WITHP_'+x]=str(withp[x]); out['GAIN_'+x]=str(greedy[x][0])
pairs=sorted(((len(covered(frozenset(p))),p) for p in itertools.combinations(EXT,2)), reverse=True)
top=[p for n,p in pairs if n==pairs[0][0]]
out['BESTPAIR']=" and ".join(f"{a} + {b}" for a,b in top)+f", tied at {pairs[0][0]}" if len(top)>1 else f"{top[0][0]} + {top[0][1]} ({pairs[0][0]})"
tr=sorted(((len(covered(frozenset(p))),p) for p in itertools.combinations(EXT,3)), reverse=True)
out['BESTTRIPLE_N']=str(tr[0][0]); out['BESTTRIPLE']=" + ".join(tr[0][1])
out['TOP3_YES']=str(s['yes']+tr[0][0]); out['TOP3_P']=pct(s['yes']+tr[0][0],N)
# catalog
def mv(r):
    if r['scale']=='yes': return ""
    if r['moves']=='none': return "none listed"
    alt=[" + ".join(x for x in a.split('+') if x!="PARAM") for a in r['moves'].split('|')]
    return " or ".join(dict.fromkeys(alt))
cat=[]
for d in doms:
    cat.append(f"### {d}\n")
    cat.append("| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |")
    cat.append("|---|---|---|---|---|---|")
    for r in [r for r in R if r['dom']==d]:
        idc=r['id'] + (f" ([{r['val']}](#{VAL[r['val']]}))" if r['val'] else "")
        ex=f"{EXPR[r['expr']]}: {r['sketch'].replace(': ', ', ')}"
        sc= f"**yes** ({r['pr']})" if r['scale']=='yes' else r['scale']
        bl= "" if r['scale']=='yes' else (OTHER_SUB[r['id']] if r['pr']=='OTHER' else REASON[r['pr']])
        cat.append(f"| {idc} | {r['text']} | {ex} | {sc} | {bl} | {mv(r)} |")
    cat.append("")
out['CATALOG']="\n".join(cat)
# per-extension lists of ids moved alone
for x in EXT:
    out['IDS_'+x]=", ".join(covered(frozenset([x]))) or "none"
out['IDS_NONE']=", ".join(r['id'] for r in R if r['moves']=='none' and r['scale']!='yes')
tmpl=open(os.path.join(HERE,'template.md')).read()
def sub(m):
    k=m.group(1)
    if k not in out: raise SystemExit("missing "+k)
    return out[k]
doc=re.sub(r"\{\{([A-Z_0-9]+)\}\}", sub, tmpl)
# insert snippets
sn=open(os.path.join(HERE,'snippets.md')).read()
blocks={}
for m in re.finditer(r"^(V\d+|T\d+|N\d+)\n\n(<!-- gocheck: run -->\n```go\n.*?\n```)\n", sn, re.S|re.M):
    blocks[m.group(1)]=m.group(2)
doc=re.sub(r"\[\[SNIP (\w\d+)\]\]", lambda m: blocks[m.group(1)], doc)
open(os.path.join(HERE,'..','invariant-coverage.md'),'w').write(doc)
print({k:v for k,v in out.items() if '\n' not in v})
print(out['EXTTABLE']); print(out['GREEDY'])
