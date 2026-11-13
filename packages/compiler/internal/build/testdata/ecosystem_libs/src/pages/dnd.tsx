import { useState } from 'react';
import { DndContext, closestCenter, PointerSensor, KeyboardSensor, useSensor, useSensors } from '@dnd-kit/core';
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy, sortableKeyboardCoordinates } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';

function Row({ id }: { id: string }) {
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id });
  return (
    <li ref={setNodeRef} id={'row-' + id} style={{ transform: CSS.Transform.toString(transform), transition }} {...attributes} {...listeners}>
      {id}
    </li>
  );
}

export default function Page() {
  const [items, setItems] = useState(['a', 'b', 'c']);
  const sensors = useSensors(useSensor(PointerSensor), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  return (
    <main>
      <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={(e) => {
        const { active, over } = e;
        if (over && active.id !== over.id) setItems((cur) => arrayMove(cur, cur.indexOf(String(active.id)), cur.indexOf(String(over.id))));
      }}>
        <SortableContext items={items} strategy={verticalListSortingStrategy}>
          <ul id="list">{items.map((i) => <Row key={i} id={i} />)}</ul>
        </SortableContext>
      </DndContext>
      <p id="order">{items.join(',')}</p>
    </main>
  );
}
