(async () => {
  const report = [];
  const $ = (s) => document.querySelector(s);
  const text = (s) => ($(s) ? $(s).textContent : '');
  const until = async (name, cond, ms = 20000) => {
    const end = Date.now() + ms;
    let last = '';
    while (Date.now() < end) {
      try {
        if (cond()) { report.push({ name, ok: true }); return true; }
      } catch (e) { last = String(e); }
      await new Promise((r) => setTimeout(r, 50));
    }
    report.push({ name, ok: false, detail: last || document.body.innerText.replace(/\n+/g, ' | ') });
    return false;
  };

  await until('host signal has its initial value', () => text('#ticks') === 'Ticks: 0');
  $('#who').click();
  await until('the host code runs on __RUNTIME__', () => text('#whoami').startsWith('__RUNTIME__ '));
  $('#bump').click();
  await until('a host call returns its value', () => text('#bumped') === '2');
  await until('the host signal updates live', () => text('#ticks') === 'Ticks: 2');
  $('#bump').click();
  await until('state persists between host calls', () => text('#bumped') === '4' && text('#ticks') === 'Ticks: 4');
  $('#count').click();
  await until('an async generator streams its values', () => text('#sum') === '10');
  $('#fail').click();
  await until('a thrown error arrives with its code', () => text('#error') === 'custom_code: nope');
  $('#echo').click();
  await until('binary and unicode arguments round trip', () => text('#echoed') === 'héllo 世界|3|255');
  return report;
})()
