const note = document.getElementById('note');
const $ = (id) => document.getElementById(id);
let currentFile = '';
let draftTimer;

function applySettings(s) {
  document.documentElement.dataset.theme = s.theme;
  document.body.style.fontSize = s.fontSize + 'px';
}

function setStatus(text) {
  $('status').textContent = text;
}

async function openFile() {
  const result = await window.scratch.openFile();
  if (!result) return;
  note.value = result.text;
  currentFile = result.path;
  $('file').textContent = currentFile;
  setStatus('opened ' + currentFile);
}

async function saveAs() {
  const filePath = await window.scratch.saveFileAs(note.value);
  if (!filePath) return;
  currentFile = filePath;
  $('file').textContent = currentFile;
  setStatus('saved ' + currentFile);
}

function newNote() {
  note.value = '';
  currentFile = '';
  $('file').textContent = '';
  setStatus('new note');
}

async function init() {
  applySettings(await window.scratch.getSettings());
  window.scratch.onSettings(applySettings);
  note.value = await window.scratch.loadDraft();

  note.addEventListener('input', () => {
    clearTimeout(draftTimer);
    draftTimer = setTimeout(() => window.scratch.saveDraft(note.value), 300);
  });
  $('open').addEventListener('click', openFile);
  $('save-as').addEventListener('click', saveAs);
  $('notify').addEventListener('click', async () => {
    const shown = await window.scratch.notify('Scratchpad', 'Note has ' + note.value.length + ' characters');
    $('notify-status').textContent = shown ? 'notification sent' : 'notifications unsupported';
  });

  window.scratch.onMenu((id) => {
    if (id === 'file.new') newNote();
    if (id === 'file.open') openFile();
    if (id === 'file.saveAs') saveAs();
  });
  window.scratch.onTray((kind) => {
    $('tray-status').textContent = 'tray ' + kind;
  });
}

init();
