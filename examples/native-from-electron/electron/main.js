// Main process: owns the windows, the menu, the tray, dialogs, files and the
// settings store. The renderer reaches it only through preload.js.
const { app, BrowserWindow, Menu, Tray, Notification, dialog, ipcMain, nativeImage } = require('electron');
const fs = require('node:fs/promises');
const path = require('node:path');

const defaults = { theme: 'light', fontSize: 16 };
let settings = { ...defaults };
let mainWindow = null;
let settingsWindow = null;
let tray = null;

const settingsFile = () => path.join(app.getPath('userData'), 'settings.json');
const draftFile = () => path.join(app.getPath('userData'), 'draft.txt');

async function loadSettings() {
  try {
    settings = { ...defaults, ...JSON.parse(await fs.readFile(settingsFile(), 'utf8')) };
  } catch {
    settings = { ...defaults };
  }
}

function createWindow(file, options = {}) {
  const win = new BrowserWindow({
    width: 900,
    height: 640,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
    },
    ...options,
  });
  win.loadFile(path.join(__dirname, 'renderer', file));
  return win;
}

function openSettings() {
  if (settingsWindow) return settingsWindow.focus();
  settingsWindow = createWindow('settings.html', { width: 420, height: 280, resizable: false, parent: mainWindow });
  settingsWindow.on('closed', () => (settingsWindow = null));
}

function sendToMain(channel, ...args) {
  if (mainWindow) mainWindow.webContents.send(channel, ...args);
}

function buildMenu() {
  const template = [
    {
      label: '&File',
      submenu: [
        { label: 'New', accelerator: 'CmdOrCtrl+N', click: () => sendToMain('menu', 'file.new') },
        { label: 'Open...', accelerator: 'CmdOrCtrl+O', click: () => sendToMain('menu', 'file.open') },
        { label: 'Save As...', accelerator: 'CmdOrCtrl+Shift+S', click: () => sendToMain('menu', 'file.saveAs') },
        { type: 'separator' },
        { label: 'Settings...', accelerator: 'CmdOrCtrl+,', click: openSettings },
        { type: 'separator' },
        { role: 'quit' },
      ],
    },
    { role: 'editMenu' },
    {
      label: '&Help',
      submenu: [
        {
          label: 'About',
          click: () => dialog.showMessageBox(mainWindow, { type: 'info', title: 'About', message: 'Scratchpad 1.0.0' }),
        },
      ],
    },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function buildTray() {
  tray = new Tray(nativeImage.createFromPath(path.join(__dirname, 'tray.png')));
  tray.setToolTip('Scratchpad');
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: 'Show', click: () => mainWindow.show() },
      { label: 'Quit', click: () => app.quit() },
    ]),
  );
  tray.on('click', () => {
    mainWindow.show();
    mainWindow.focus();
    sendToMain('tray', 'click');
  });
}

// IPC: one handler per call the renderer may make.
ipcMain.handle('file:open', async () => {
  const { canceled, filePaths } = await dialog.showOpenDialog(mainWindow, {
    filters: [{ name: 'Text', extensions: ['txt', 'md'] }],
    properties: ['openFile'],
  });
  if (canceled || filePaths.length === 0) return null;
  return { path: filePaths[0], text: await fs.readFile(filePaths[0], 'utf8') };
});

ipcMain.handle('file:saveAs', async (_event, text) => {
  const { canceled, filePath } = await dialog.showSaveDialog(mainWindow, { defaultPath: 'note.txt' });
  if (canceled || !filePath) return null;
  await fs.writeFile(filePath, text, 'utf8');
  return filePath;
});

ipcMain.handle('draft:load', async () => {
  try {
    return await fs.readFile(draftFile(), 'utf8');
  } catch {
    return '';
  }
});

ipcMain.handle('draft:save', (_event, text) => fs.writeFile(draftFile(), text, 'utf8'));

ipcMain.handle('settings:get', () => settings);

ipcMain.handle('settings:update', async (_event, patch) => {
  const next = { ...settings, ...patch };
  if (next.theme !== 'light' && next.theme !== 'dark') throw new Error('theme must be light or dark');
  if (!(next.fontSize >= 10 && next.fontSize <= 32)) throw new Error('fontSize must be 10 to 32');
  settings = next;
  await fs.writeFile(settingsFile(), JSON.stringify(settings), 'utf8');
  // Tell every window, so the main window follows the settings window.
  for (const win of BrowserWindow.getAllWindows()) win.webContents.send('settings', settings);
  return settings;
});

ipcMain.handle('notify', (_event, title, body) => {
  if (!Notification.isSupported()) return false;
  const n = new Notification({ title, body });
  n.on('click', () => {
    mainWindow.show();
    mainWindow.focus();
  });
  n.show();
  return true;
});

app.whenReady().then(async () => {
  await loadSettings();
  buildMenu();
  mainWindow = createWindow('index.html');
  buildTray();
});

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit();
});
