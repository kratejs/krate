'use client';
import { toast } from 'sonner';

export default function SaveButton() {
  return <button id="save" onClick={() => toast('Saved from page')}>save</button>;
}
