// The only bridge between the page and the main process. contextIsolation is
// on, so the page sees window.scratch and nothing else of Electron or Node.
const { contextBridge, ipcRenderer } = require('electron');

function subscribe(channel, fn) {
  const listener = (_event, ...args) => fn(...args);
  ipcRenderer.on(channel, listener);
  return () => ipcRenderer.removeListener(channel, listener);
}

contextBridge.exposeInMainWorld('scratch', {
  openFile: () => ipcRenderer.invoke('file:open'),
  saveFileAs: (text) => ipcRenderer.invoke('file:saveAs', text),
  loadDraft: () => ipcRenderer.invoke('draft:load'),
  saveDraft: (text) => ipcRenderer.invoke('draft:save', text),
  getSettings: () => ipcRenderer.invoke('settings:get'),
  updateSettings: (patch) => ipcRenderer.invoke('settings:update', patch),
  notify: (title, body) => ipcRenderer.invoke('notify', title, body),
  onSettings: (fn) => subscribe('settings', fn),
  onMenu: (fn) => subscribe('menu', fn),
  onTray: (fn) => subscribe('tray', fn),
});
