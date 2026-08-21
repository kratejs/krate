import './file-tree.css';
import { createSignal, createEffect, onMount } from '@krate/runtime';

export interface FileTreeProps {
  children?: any;
  defaultOpen?: boolean;
}

export default function FileTree(props: FileTreeProps) {
  var children = props.children || "";
  return (
    <div class="krate-file-tree">
      <div class="krate-file-tree-header">
        <span class="krate-file-tree-header-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.93a2 2 0 0 1-1.66-.9l-.82-1.2A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13c0 1.1.9 2 2 2Z"/>
          </svg>
        </span>
        <span>Files</span>
      </div>
      <div class="krate-file-tree-content">
        {children}
      </div>
    </div>
  );
}

export interface FileTreeItemProps {
  children?: any;
  icon?: string;
}

export function FileTreeItem(props: FileTreeItemProps) {
  var children = props.children || "";
  return (
    <div class="krate-file-tree-item">
      <span class="krate-file-tree-item-icon">{props.icon || "📄"}</span>
      <span class="krate-file-tree-item-name">{children}</span>
    </div>
  );
}

export interface FileTreeFolderProps {
  children?: any;
  name?: string;
  defaultOpen?: boolean;
}

export function FileTreeFolder(props: FileTreeFolderProps) {
  var children = props.children || "";
  var [open, setOpen] = createSignal(props.defaultOpen !== false);

  function toggle() {
    setOpen(!open());
  }

  return (
    <div class={"krate-file-tree-folder" + (open() ? " krate-file-tree-folder-open" : "")}>
      <div class="krate-file-tree-item krate-file-tree-folder-trigger" onClick={toggle}>
        <span class="krate-file-tree-item-icon krate-file-tree-chevron">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m9 18 6-6-6-6"/>
          </svg>
        </span>
        <span class="krate-file-tree-item-icon">{open() ? "📂" : "📁"}</span>
        <span class="krate-file-tree-item-name">{props.name || ""}</span>
      </div>
      <div class={"krate-file-tree-children" + (open() ? "" : " krate-file-tree-children-closed")}>
        {children}
      </div>
    </div>
  );
}
