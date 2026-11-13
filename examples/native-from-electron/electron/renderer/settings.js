const $ = (id) => document.getElementById(id);

function show(s) {
  document.documentElement.dataset.theme = s.theme;
  $('theme').value = s.theme;
  $('font-size').value = s.fontSize;
}

async function update(patch) {
  try {
    $('error').textContent = '';
    await window.scratch.updateSettings(patch);
  } catch (e) {
    $('error').textContent = String(e.message);
  }
}

async function init() {
  show(await window.scratch.getSettings());
  window.scratch.onSettings(show);
  $('theme').addEventListener('change', (e) => update({ theme: e.target.value }));
  $('font-size').addEventListener('change', (e) => update({ fontSize: Number(e.target.value) }));
}

init();
