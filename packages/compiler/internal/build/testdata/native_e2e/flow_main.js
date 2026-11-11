(async () => {
  const report = [];
  const $ = (s) => document.querySelector(s);
  const text = (s) => ($(s) ? $(s).textContent : '');
  const until = async (name, cond, ms = 8000) => {
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
  const rows = () => document.querySelectorAll('#list li').length;

  await until('lists the tasks from Go List()', () => rows() === 1 && text('#list').includes('Read the Krate Native docs'));
  await until('baked @build TS value is on the page', () => /^Built \d{4}-\d{2}-\d{2}$/.test(text('#build-ts')));
  await until('baked //krate:build Go value is on the page', () => text('#build-go').startsWith('Krate Native Tasks edition, built with go'));
  await until('host signal Count() renders', () => text('#open') === 'Open tasks: 1');

  const input = $('#title');
  input.value = 'second task';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  $('#add').click();
  await until('Add shows the new task', () => rows() === 2 && text('#list').includes('second task'));
  await until('Count() updates live after Add', () => text('#open') === 'Open tasks: 2');
  await until('the input is cleared after Add', () => $('#title').value === '');

  document.querySelector('#list .toggle').click();
  await until('Toggle marks the task done', () => $('#list .toggle').textContent === 'done');
  await until('Count() updates live after Toggle', () => text('#open') === 'Open tasks: 1');

  $('#watch').click();
  await until('Go stream Watch delivers updates', () => text('#seen') === 'Tasks seen by the stream: 2');

  $('#note').click();
  await until('TS host hostSignal updates live', () => text('#notes') === 'Notes: 1');
  $('#last').click();
  await until('TS host code wrote kv in-process and reads it back', () => text('#last-note') === 'hello');
  $('#greet').click();
  await until('TS host function returns a value', () => text('#greeting') === 'Hello, world, from the host.');

  $('#missing').click();
  await until('Go error arrives as KrateError with its code', () => text('#error').startsWith('not_found: '));

  document.querySelector('a[href="/tasks/2"]').click();
  await until('client navigation reaches /tasks/2', () => location.pathname === '/tasks/2');
  await until('client navigation renders the right task', () => text('#heading') === 'Task 2' && text('#title') === 'second task');
  $('#back').click();
  await until('client navigation back to the list', () => location.pathname === '/' && rows() === 2);

  return report;
})()
