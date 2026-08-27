import './file-tree.css';
import { createSignal, createEffect, onMount } from '@krate/runtime';

export interface FileTreeProps {
  children?: any;
  defaultOpen?: boolean;
}

export function FileTree(props: FileTreeProps) {
  var children = props.children || "";
  return (
    <div class="krate-file-tree">
      <div class="krate-file-tree-header">
        <span class="krate-file-tree-header-icon">
          <Icon name="tabler:folder" width="14" height="14" />
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
  var icon = props.icon || "tabler:file";
  return (
    <div class="krate-file-tree-item">
      <span class="krate-file-tree-item-icon">
        <Icon name={icon} width="14" height="14" />
      </span>
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
          <Icon name="tabler:chevron-right" width="12" height="12" />
        </span>
        <span class="krate-file-tree-item-icon">
          {open() ? 
            <Icon name="tabler:folder-open" width="14" height="14" />
           : 
            <Icon name="tabler:folder" width="14" height="14" />
          }
        </span>
        <span class="krate-file-tree-item-name">{props.name || ""}</span>
      </div>
      <div class={"krate-file-tree-children" + (open() ? "" : " krate-file-tree-children-closed")}>
        {children}
      </div>
    </div>
  );
}
