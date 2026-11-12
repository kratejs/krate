'use strict';
const { contextBridge, ipcRenderer } = require('electron');

contextBridge.exposeInMainWorld('host', {
  ping: (n) => ipcRenderer.invoke('bench:ping', n),
  report: (record) => ipcRenderer.invoke('bench:report', record),
});
