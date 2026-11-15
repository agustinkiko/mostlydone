// Board: a column per state. Dragging a card sends one PATCH with `state`
// and `sort_order`, applied optimistically and rolled back on failure.

import { useMemo, useRef, useState } from "react";
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, closestCenter, pointerWithin, useDraggable, useDroppable, useSensor, useSensors,
  type CollisionDetection, type DragEndEvent, type DragOverEvent, type DragStartEvent,
} from "@dnd-kit/core";
import { errorText } from "../api/client";
import { issues as issuesApi } from "../api/endpoints";
import type { Issue, Project, State } from "../api/types";
import { issueKey, useCanWrite } from "../app/session";
import { ErrorBox, Loading, toast } from "../components/bits";
import { StateIcon } from "../components/Icons";
import { emitIssue } from "../lib/issueEvents";
import { computeDrop, sortCards } from "../lib/sortOrder";
import { ItemMeta, onItemClick, QuickAdd, useLookups, type Lookups } from "./itemBits";
import type { ItemsViewProps } from "./ItemsRoute";

type OverInfo = { column: string; cardId?: string; after?: boolean };

// Prefer the card under the pointer, then the column under it, then the
// nearest target (keyboard dragging has no pointer).
const collision: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  if (hits.length) {
    const card = hits.find((c) => String(c.id).startsWith("card:"));
    return card ? [card] : hits;
  }
  return closestCenter(args);
};

export default function ItemsBoard({ project, items, setItems, hrefFor, openItem, cycleId, moduleId }: ItemsViewProps) {
  const lk = useLookups(project.id);
  const canWrite = useCanWrite();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [over, setOver] = useState<OverInfo | null>(null);
  const justDragged = useRef(false);
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor),
  );

  const columns = useMemo(() => {
    const cols: Record<string, Issue[]> = {};
    for (const s of lk.states) cols[s.id] = [];
    for (const i of sortCards(items)) (cols[i.state] ??= []).push(i);
    return cols;
  }, [items, lk.states]);

  if (lk.error) return <div style={{ padding: 16 }}><ErrorBox error={lk.error} /></div>;
  if (lk.loading) return <Loading />;

  const overInfo = (e: DragOverEvent | DragEndEvent): OverInfo | null => {
    const o = e.over;
    if (!o) return null;
    const data = o.data.current as { column: string; cardId?: string } | undefined;
    if (!data) return null;
    if (!data.cardId) return { column: data.column };
    const a = e.active.rect.current.translated;
    const after = a ? a.top + a.height / 2 > o.rect.top + o.rect.height / 2 : false;
    return { column: data.column, cardId: data.cardId, after };
  };

  const onStart = (e: DragStartEvent) => setActiveId(String(e.active.id));
  const onOver = (e: DragOverEvent) => setOver(overInfo(e));
  const onEnd = async (e: DragEndEvent) => {
    const id = String(e.active.id);
    const info = overInfo(e);
    setActiveId(null);
    setOver(null);
    justDragged.current = true;
    setTimeout(() => (justDragged.current = false), 0);
    if (!info) return;
    const drop = computeDrop(columns, id, info);
    const before = items.find((i) => i.id === id);
    if (!drop || !before) return;
    const patch = drop.column === before.state ? { sort_order: drop.sortOrder } : { state: drop.column, sort_order: drop.sortOrder };
    setItems((list) => list?.map((i) => (i.id === id ? { ...i, state: drop.column, sort_order: drop.sortOrder } : i)));
    try {
      const saved = await issuesApi.update(project.id, id, patch);
      emitIssue({ type: "saved", issue: saved });
    } catch (x) {
      setItems((list) => list?.map((i) => (i.id === id ? before : i)));
      toast(`Could not move ${issueKey(project, before.sequence_id)}: ${errorText(x)}`, "error");
    }
  };

  const active = activeId ? items.find((i) => i.id === activeId) : undefined;
  const defaults = (stateId: string) => ({
    state: stateId,
    ...(cycleId ? { cycle_id: cycleId } : {}),
    ...(moduleId ? { module_ids: [moduleId] } : {}),
  });

  return (
    <DndContext sensors={sensors} collisionDetection={collision} onDragStart={onStart} onDragOver={onOver} onDragEnd={onEnd}
      onDragCancel={() => { setActiveId(null); setOver(null); }}
      accessibility={{
        screenReaderInstructions: { draggable: "To move a work item, press space or enter, use the arrow keys, then press space or enter again to drop. Press escape to cancel." },
      }}
    >
      <div className="board">
        {lk.states.map((s) => (
          <Column key={s.id} state={s} cards={columns[s.id] ?? []} isOver={over?.column === s.id && !over.cardId}>
            {(columns[s.id] ?? []).map((i) => (
              <DraggableCard
                key={i.id} issue={i} project={project} lk={lk} href={hrefFor(issueKey(project, i.sequence_id))} disabled={!canWrite}
                dropHint={over?.cardId === i.id && activeId !== i.id ? (over.after ? "after" : "before") : undefined}
                onOpen={() => !justDragged.current && openItem(issueKey(project, i.sequence_id))}
              />
            ))}
            {canWrite && <QuickAdd project={project} defaults={defaults(s.id)} className="quick-add" />}
          </Column>
        ))}
      </div>
      <DragOverlay>{active ? <CardBody issue={active} project={project} lk={lk} className="card overlay" /> : null}</DragOverlay>
    </DndContext>
  );
}

function Column({ state, cards, isOver, children }: { state: State; cards: Issue[]; isOver: boolean; children: React.ReactNode }) {
  const { setNodeRef } = useDroppable({ id: `col:${state.id}`, data: { column: state.id } });
  return (
    <section className={`column${isOver ? " over" : ""}`} aria-label={`${state.name}, ${cards.length} items`}>
      <div className="column-head">
        <StateIcon group={state.group} color={state.color} />
        <span>{state.name}</span>
        <span className="muted small">{cards.length}</span>
      </div>
      <div ref={setNodeRef} className="column-body">{children}</div>
    </section>
  );
}

interface CardProps {
  issue: Issue;
  project: Project;
  lk: Lookups;
  href: string;
  disabled: boolean;
  dropHint?: "before" | "after";
  onOpen: () => void;
}

function DraggableCard({ issue, project, lk, href, disabled, dropHint, onOpen }: CardProps) {
  const drag = useDraggable({ id: issue.id, disabled });
  const drop = useDroppable({ id: `card:${issue.id}`, data: { column: issue.state, cardId: issue.id } });
  const setRef = (el: HTMLElement | null) => {
    drag.setNodeRef(el);
    drop.setNodeRef(el);
  };
  const cls = `card${drag.isDragging ? " dragging" : ""}${dropHint ? ` drop-${dropHint}` : ""}`;
  return (
    <a
      ref={setRef} href={href} className={cls} {...drag.attributes} {...drag.listeners}
      aria-roledescription={disabled ? undefined : "draggable work item"}
      onClick={(e) => onItemClick(e, onOpen)}
      onKeyDown={(e) => {
        // Enter opens the item; space starts a keyboard drag.
        if (e.key === "Enter") return;
        drag.listeners?.onKeyDown?.(e);
      }}
    >
      <CardInner issue={issue} project={project} lk={lk} />
    </a>
  );
}

function CardInner({ issue, project, lk }: { issue: Issue; project: Project; lk: Lookups }) {
  return (
    <>
      <div className="card-key">{issueKey(project, issue.sequence_id)}</div>
      <div className="card-name">{issue.name}</div>
      <div className="card-meta"><ItemMeta issue={issue} lk={lk} compact /></div>
    </>
  );
}

function CardBody(props: { issue: Issue; project: Project; lk: Lookups; className: string }) {
  return <div className={props.className}><CardInner {...props} /></div>;
}
