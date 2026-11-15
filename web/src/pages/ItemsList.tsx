import { useMemo, useState } from "react";
import { issueKey, useCanWrite } from "../app/session";
import { Empty, ErrorBox, Loading } from "../components/bits";
import { IconChevron, StateIcon } from "../components/Icons";
import { prefs } from "../lib/prefs";
import { sortCards } from "../lib/sortOrder";
import type { Issue } from "../api/types";
import { ItemMeta, onItemClick, QuickAdd, useLookups } from "./itemBits";
import type { ItemsViewProps } from "./ItemsRoute";

export function ItemsList({ project, items, hrefFor, openItem, selectedKey, cycleId, moduleId }: ItemsViewProps) {
  const lk = useLookups(project.id);
  const canWrite = useCanWrite();
  const [collapsed, setCollapsed] = useState<string[]>(() => prefs.collapsed(project.id));
  const byState = useMemo(() => {
    const m = new Map<string, Issue[]>();
    for (const i of sortCards(items)) {
      const l = m.get(i.state);
      if (l) l.push(i);
      else m.set(i.state, [i]);
    }
    return m;
  }, [items]);

  if (lk.error) return <div style={{ padding: 16 }}><ErrorBox error={lk.error} /></div>;
  if (lk.loading) return <Loading />;

  const toggle = (id: string) => {
    const next = collapsed.includes(id) ? collapsed.filter((x) => x !== id) : [...collapsed, id];
    setCollapsed(next);
    prefs.setCollapsed(project.id, next);
  };
  const defaults = (stateId: string) => ({
    state: stateId,
    ...(cycleId ? { cycle_id: cycleId } : {}),
    ...(moduleId ? { module_ids: [moduleId] } : {}),
  });

  return (
    <div>
      {items.length === 0 && (
        <Empty title="No work items here">
          <p>{canWrite ? "Add one below, or press C to open the new work item form." : "Nothing matches these filters."}</p>
        </Empty>
      )}
      {lk.states.map((s) => {
        const list = byState.get(s.id) ?? [];
        const open = !collapsed.includes(s.id);
        if (!list.length && items.length && !canWrite) return null;
        return (
          <section key={s.id} aria-label={`${s.name}, ${list.length} items`}>
            <div className="group-head">
              <button type="button" className="group-toggle" aria-expanded={open} onClick={() => toggle(s.id)}>
                <IconChevron className="chev" size={14} />
                <StateIcon group={s.group} color={s.color} />
                {s.name}
              </button>
              <span className="count">{list.length}</span>
            </div>
            {open && (
              <div role="list">
                {list.map((i) => {
                  const key = issueKey(project, i.sequence_id);
                  return (
                    <a
                      key={i.id} role="listitem" href={hrefFor(key)} className={`item-row${selectedKey === key ? " selected" : ""}`}
                      onClick={(e) => onItemClick(e, () => openItem(key))}
                    >
                      <span className="item-key">{key}</span>
                      <StateIcon group={s.group} color={s.color} />
                      <span className="item-name">{i.name}</span>
                      <span className="item-meta"><ItemMeta issue={i} lk={lk} /></span>
                    </a>
                  );
                })}
                {canWrite && <QuickAdd project={project} defaults={defaults(s.id)} />}
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}
