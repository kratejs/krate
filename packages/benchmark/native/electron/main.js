'use strict';
// Electron side of the benchmark. contextIsolation is on; the page talks to
// the main process only through the two functions preload.js exposes.
const { app, BrowserWindow, ipcMain } = require('electron');
const fs = require('node:fs');
const path = require('node:path');

ipcMain.handle('bench:ping', (_e, n) => `pong ${process.platform} ${Number(n) + 1}`);
ipcMain.handle('bench:report', (_e, record) => {
  const file = process.env.BENCH_REPORT;
  if (file) fs.writeFileSync(file, `${String(record)}\n`);
});

app.whenReady().then(() => {
  const win = new BrowserWindow({
    width: 800,
    height: 600,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
  });
  win.loadFile(path.join(__dirname, 'index.html'));
});

app.on('window-all-closed', () => app.quit());
