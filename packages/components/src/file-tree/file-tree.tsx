import './file-tree.css';


export interface FileTreeProps {
  children?: any;
}

/** File tree container. Zero JS (folders use native `<details>`). */
export function FileTree(props: FileTreeProps) {
  return <div class="krate-file-tree">{props.children}</div>;
}

export interface FileTreeItemProps {
  children?: any;
}

/** @deprecated Use `FileTree.Item` instead. */
export function FileTreeItem(props: FileTreeItemProps) {
  return (
    <div class="krate-file-tree-item">
      <span class="krate-file-tree-icon" aria-hidden="true">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
          <path d="M14 2v6h6" />
        </svg>
      </span>
      <span class="krate-file-tree-name">{props.children}</span>
    </div>
  );
}

export interface FileTreeFolderProps {
  name?: string;
  defaultOpen?: boolean;
  children?: any;
}

/** @deprecated Use `FileTree.Folder` instead. */
export function FileTreeFolder(props: FileTreeFolderProps) {
  return (
    <details class="krate-file-tree-folder" open={props.defaultOpen}>
      <summary class="krate-file-tree-folder-header">
        <span class="krate-file-tree-chevron" aria-hidden="true">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="m9 18 6-6-6-6" />
          </svg>
        </span>
        <span class="krate-file-tree-icon" aria-hidden="true">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 20h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" />
          </svg>
        </span>
        <span class="krate-file-tree-name">{props.name}</span>
      </summary>
      <div class="krate-file-tree-children">{props.children}</div>
    </details>
  );
}

export interface FileTree {
  Item: typeof FileTreeItem;
  Folder: typeof FileTreeFolder;
}

FileTree.Item = FileTreeItem;
FileTree.Folder = FileTreeFolder;
