(async () => {
  const report = [];
  const $ = (s) => document.querySelector(s);
  const text = (s) => ($(s) ? $(s).textContent : '');
  const until = async (name, cond, ms = 8000) => {
    const end = Date.now() + ms;
    while (Date.now() < end) {
      try {
        if (cond()) { report.push({ name, ok: true }); return true; }
      } catch (e) {}
      await new Promise((r) => setTimeout(r, 50));
    }
    report.push({ name, ok: false, detail: location.pathname + ' ' + document.body.innerText.replace(/\n+/g, ' | ') });
    return false;
  };
  const found = location.pathname === '/tasks/2';
  if (found) {
    await until('direct load renders the right task', () => text('#heading') === 'Task 2' && text('#title') === 'second task' && text('#error') === '');
  } else {
    await until('direct load of a missing task reports not_found', () => text('#heading') === 'Task 77' && text('#error') === 'not_found');
  }
  return report;
})()
