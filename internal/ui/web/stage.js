'use strict';

(() => {
  if (window.__dofirstWindow !== 'stage') return;
  const { call, on, t, plural, h, icon, logo, clock, errorText } = DF;
  const root = document.getElementById('root');

  const S = {
    boot: { hotkey: 'Ctrl+Alt+D', version: '' },
    view: null,
    mode: 'overlay',
    dialog: null,            // 'disable' | null
    disableMode: 'tomorrow',
    disableReason: '',
    drafts: {},              // id задачи
    error: null,             // {id, text}
    toast: null,
    warn: 0,                 // секунд до закрытия вкладки
    update: null,            // {version, busy, error}
    busy: false,
  };
  let lastKey = '';
  let toastTimer = 0;

  // ---------- действия ----------------------------------------------------

  function apply(view) {
    if (view) S.view = view;
    render();
  }

  function act(method, params, taskId) {
    S.busy = true;
    S.error = null;
    render();
    return call(method, params)
      .then(apply)
      .catch((err) => { S.error = { id: taskId || '', text: errorText(err) }; })
      .finally(() => { S.busy = false; render(); });
  }

  function toast(text) {
    S.toast = text;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { S.toast = null; render(); }, 5000);
    render();
  }

  const setMode = (mode) => call('stage.mode', { mode });

  function openDisable() {
    S.dialog = 'disable';
    S.disableMode = S.view && S.view.phase === 'night' ? 'night' : 'tomorrow';
    S.disableReason = '';
    render();
  }

  function confirmDisable() {
    const params = { mode: S.disableMode, reason: S.disableReason };
    S.dialog = null;
    act('lock.disable', params);
  }

  // ---------- левая колонка -----------------------------------------------

  function side(v) {
    const now = new Date(v.now);
    const lang = DF.getLang();
    const weekday = new Intl.DateTimeFormat(lang, { weekday: 'long' }).format(now);
    const date = new Intl.DateTimeFormat(lang, { day: 'numeric', month: 'long' }).format(now);

    return h('aside', { class: 'side' },
      h('div', { class: 'brand' }, logo(30), h('span', {}, 'DoFirstTo')),
      h('div', { class: 'today' },
        h('p', { class: 'motto' }, t('motto')),
        h('h1', { class: 'weekday' }, weekday),
        h('p', { class: 'date' }, date)),
      h('div', { class: 'facts' },
        h('div', { class: 'fact' },
          h('span', { class: 'fact-label' }, t('streak')),
          v.streak > 0
            ? h('span', { class: 'fact-value' }, h('b', {}, v.streak), ' ', plural(v.streak, 'days'))
            : h('span', { class: 'fact-quiet' }, t('streak.none'))),
        h('div', { class: 'fact' },
          h('span', { class: 'fact-label' }, t('open.now')),
          v.openGroups.length
            ? h('div', { class: 'chips' }, v.openGroups.map((g) => h('span', { class: 'chip on' }, g)))
            : h('span', { class: 'fact-quiet' }, t('open.none')))),
      h('footer', { class: 'side-foot' },
        h('div', { class: 'side-actions' },
          h('button', { class: 'btn primary', onclick: () => setMode('widget') }, t('btn.work'), icon('arrow', 16)),
          h('button', { class: 'btn quiet', onclick: openDisable }, icon('power', 16), t('btn.disable'))),
        h('p', { class: 'hint' }, t('hint.hotkey', { k: S.boot.hotkey }))));
  }

  // ---------- нить задач --------------------------------------------------

  const RING = 2 * Math.PI * 25;

  function progress(task) {
    const total = task.minMinutes * 60;
    return total > 0 ? Math.min(1, Math.max(0, 1 - task.remainingSec / total)) : 1;
  }

  function ring(task) {
    const el = h('span', { class: 'ring' });
    el.innerHTML = '<svg viewBox="0 0 60 60" width="60" height="60">' +
      '<circle class="ring-track" cx="30" cy="30" r="25"/>' +
      '<circle class="ring-bar" cx="30" cy="30" r="25" transform="rotate(-90 30 30)" stroke-dasharray="' + RING +
      '" stroke-dashoffset="' + RING * (1 - progress(task)) + '" data-ring="' + task.id + '"/></svg>';
    return el;
  }

  function node(task, index) {
    if (task.status === 'done') return h('span', { class: 'node' }, icon('check', 14));
    if (task.status === 'active') {
      return task.kind === 'timer'
        ? h('span', { class: 'node big' }, ring(task))
        : h('span', { class: 'node big plain' }, h('i', { class: 'pulse' }));
    }
    if (task.status === 'locked') return h('span', { class: 'node' }, icon('lock', 12));
    return h('span', { class: 'node' }, index + 1);
  }

  function chipRow(label, names) {
    if (!names.length) return null;
    return h('div', { class: 'chip-row' },
      h('span', { class: 'chip-label' }, label),
      names.map((n) => h('span', { class: 'chip' }, n)));
  }

  function launchName(target) {
    const url = /^https?:\/\/([^/]+)/i.exec(target);
    if (url) return url[1].replace(/^www\./, '');
    return target.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || target;
  }

  function activeBody(task, v) {
    const left = task.remainingSec;
    const timer = task.kind === 'timer';
    const out = [];

    if (task.description) out.push(h('p', { class: 'task-desc' }, task.description));
    out.push(chipRow(t('task.needs'), task.needs));

    if (task.launch.length) {
      out.push(h('div', { class: 'launch' }, task.launch.map((target, i) =>
        h('button', { class: 'btn line', title: target, onclick: () => act('task.launch', { id: task.id, index: i }, task.id) },
          t('task.launch'), ' ', launchName(target), icon('out', 15)))));
    }

    if (timer) {
      const paused = v.paused && left > 0;
      const state = left <= 0 ? t('timer.ready') : paused ? t('timer.paused') : t('timer.left', { t: clock(left) });
      out.push(h('div', { class: 'timer-row' },
        h('p', { class: 'timer-line' + (paused ? ' paused' : '') },
          icon(paused ? 'pause' : 'clock', 15), h('span', { 'data-left': task.id }, state)),
        left > 0 ? h('button', {
          class: 'btn line small', disabled: S.busy,
          onclick: () => act('task.pause', { paused: !v.paused }, task.id),
        }, icon(paused ? 'play' : 'pause', 14), paused ? t('timer.resume') : t('timer.pause')) : null));
    }

    if (task.requireNote) {
      out.push(h('textarea', {
        class: 'note', rows: 3, placeholder: t('task.note'), 'data-focus': 'note-' + task.id,
        value: S.drafts[task.id] || '',
        oninput: (e) => { S.drafts[task.id] = e.target.value; },
      }));
    }

    if (S.error && S.error.id === task.id) out.push(h('p', { class: 'error' }, S.error.text));

    out.push(h('div', { class: 'task-actions' },
      h('button', {
        class: 'btn primary big', disabled: left > 0 || S.busy,
        onclick: () => act('task.complete', { id: task.id, note: S.drafts[task.id] || '' }, task.id),
      }, icon('check', 18), h('span', { 'data-done': task.id }, left > 0 ? clock(left) : t('task.done'))),
      task.requireNote ? h('span', { class: 'hint' }, t('task.note.must')) : null));
    return out;
  }

  function taskItem(task, index, v) {
    const body = h('div', { class: 'task-body' },
      h('div', { class: 'task-head' },
        h('h2', { class: 'task-title' }, task.title),
        task.kind === 'timer' && task.status !== 'done'
          ? h('span', { class: 'badge' }, icon('clock', 13), task.minMinutes + ' ' + t('min')) : null));

    if (task.status === 'done') {
      if (task.note) body.append(h('p', { class: 'task-note' }, task.note));
    } else if (task.status === 'active') {
      body.append(...activeBody(task, v).filter(Boolean));
    } else {
      if (task.description) body.append(h('p', { class: 'task-desc' }, task.description));
      const row = chipRow(t('task.unlocks'), task.needs.concat(task.unlocks.filter((u) => !task.needs.includes(u))));
      if (row) body.append(row);
      if (task.status === 'ready') {
        body.append(h('div', { class: 'task-actions' },
          h('button', { class: 'btn line', disabled: S.busy, onclick: () => act('task.activate', { id: task.id }, task.id) }, t('task.start'))));
        if (S.error && S.error.id === task.id) body.append(h('p', { class: 'error' }, S.error.text));
      }
    }
    return h('li', { class: 'task ' + task.status }, node(task, index), body);
  }

  // ---------- диалог выключения ------------------------------------------

  function disableDialog(v) {
    const modes = v.phase === 'night' ? ['night', 'tomorrow', 'quit'] : ['tomorrow', 'quit'];
    return h('div', { class: 'scrim', onclick: (e) => { if (e.target.classList.contains('scrim')) { S.dialog = null; render(); } } },
      h('div', { class: 'dialog', role: 'dialog' },
        h('h2', {}, t('disable.title')),
        h('div', { class: 'options' }, modes.map((m) =>
          h('label', { class: 'option' + (S.disableMode === m ? ' on' : '') },
            h('input', { type: 'radio', name: 'mode', checked: S.disableMode === m, onchange: () => { S.disableMode = m; render(); } }),
            h('span', {}, h('b', {}, t('disable.' + m)), h('small', {}, t('disable.' + m + '.hint')))))),
        h('label', { class: 'field' },
          h('span', {}, t('disable.reason')),
          h('input', {
            type: 'text', maxlength: 200, placeholder: t('disable.reason.ph'), 'data-focus': 'reason',
            value: S.disableReason, oninput: (e) => { S.disableReason = e.target.value; },
            onkeydown: (e) => { if (e.key === 'Enter') confirmDisable(); },
          })),
        h('div', { class: 'dialog-actions' },
          h('button', { class: 'btn quiet', onclick: () => { S.dialog = null; render(); } }, t('cancel')),
          h('button', { class: 'btn danger', onclick: confirmDisable }, t('disable.go')))));
  }

  // ---------- экраны ------------------------------------------------------

  function sheet(v) {
    return h('div', { class: 'sheet' },
      side(v),
      h('main', { class: 'main' },
        v.test ? h('div', { class: 'banner' }, h('span', {}, t('test.banner')),
          h('button', { class: 'btn line small', onclick: () => act('lock.disable', { mode: 'tomorrow', reason: '' }) }, t('test.stop'))) : null,
        h('ol', { class: 'thread' },
          v.tasks.map((task, i) => taskItem(task, i, v)),
          h('li', { class: 'task finish' },
            h('span', { class: 'node' }, icon('check', 14)),
            h('div', { class: 'task-body' },
              h('h2', { class: 'task-title' }, t('finish.title')),
              h('p', { class: 'task-desc' }, t('finish.sub')))))));
  }

  function nightScreen(v) {
    const wake = new Intl.DateTimeFormat(DF.getLang(), { hour: '2-digit', minute: '2-digit' }).format(new Date(v.next));
    return h('div', { class: 'center night' },
      h('div', { class: 'moon' }, icon('moon', 56)),
      h('h1', {}, t('night.title')),
      h('p', { class: 'lead' }, t('night.sub', { time: wake })),
      v.openGroups.length ? h('div', { class: 'chip-row' },
        h('span', { class: 'chip-label' }, t('open.now')),
        v.openGroups.map((g) => h('span', { class: 'chip on' }, g))) : null,
      h('div', { class: 'row' },
        h('button', { class: 'btn line', disabled: S.busy, onclick: () => act('night.snooze', { minutes: 15 }) }, t('night.snooze')),
        h('button', { class: 'btn quiet', onclick: openDisable }, icon('power', 16), t('btn.disable'))));
  }

  function doneScreen(v) {
    return h('div', { class: 'center done' },
      h('div', { class: 'seal' }, logo(120)),
      h('h1', {}, t('done.title')),
      h('p', { class: 'lead' }, t('done.sub')),
      v.streak > 0 ? h('p', { class: 'streak-big' }, t('streak'), ' · ', h('b', {}, v.streak), ' ', plural(v.streak, 'days')) : null,
      h('button', { class: 'btn primary big', onclick: () => setMode('hidden') }, t('done.ok')));
  }

  function updateCard() {
    const u = S.update;
    return h('div', { class: 'update' },
      h('div', { class: 'update-head' }, logo(34), h('div', {},
        h('h2', {}, t('update.title')),
        h('p', {}, t('update.sub', { v: u.version, cur: S.boot.version })))),
      u.error ? h('p', { class: 'error' }, u.error) : null,
      u.busy ? h('p', { class: 'hint' }, t('update.busy')) : h('div', { class: 'row' },
        h('button', { class: 'btn primary', onclick: () => {
          u.busy = true; u.error = null; render();
          call('update.apply').catch((err) => { u.busy = false; u.error = errorText(err); render(); });
        } }, t('update.go')),
        h('button', { class: 'btn quiet', onclick: () => { S.update = null; call('update.later'); render(); } }, t('update.later'))));
  }

  function widget(v) {
    const expand = () => setMode('overlay');
    if (S.warn > 0) {
      return h('button', { class: 'widget warn', onclick: expand }, icon('warn', 26),
        h('span', { class: 'w-text' }, h('b', {}, t('widget.warn')),
          h('small', { 'data-warn': '1' }, t('widget.warn.sub', { n: S.warn }))));
    }
    if (v.phase === 'night') {
      return h('button', { class: 'widget', onclick: expand }, icon('moon', 26),
        h('span', { class: 'w-text' }, h('b', {}, t('widget.night'))), icon('expand', 16));
    }
    const task = v.tasks.find((x) => x.id === v.active);
    if (!task) {
      return h('button', { class: 'widget', onclick: expand }, logo(30),
        h('span', { class: 'w-text' }, h('b', {}, t('widget.pick'))), icon('expand', 16));
    }
    const timer = task.kind === 'timer';
    const sub = !timer ? t('widget.check')
      : task.remainingSec <= 0 ? t('timer.ready')
        : v.paused ? t('timer.paused') : t('timer.left', { t: clock(task.remainingSec) });
    return h('button', { class: 'widget', onclick: expand },
      timer ? h('span', { class: 'w-ring' }, ring(task)) : logo(30),
      h('span', { class: 'w-text' }, h('b', {}, task.title), h('small', { 'data-left': task.id }, sub)),
      icon('expand', 16));
  }

  // ---------- отрисовка ---------------------------------------------------

  function build() {
    const v = S.view;
    if (!v) return h('div', { class: 'center' });
    if (S.mode === 'widget') return widget(v);
    if (S.mode === 'dialog') return S.update ? h('div', { class: 'center' }, updateCard()) : h('div', { class: 'center' });

    let screen;
    if (v.phase === 'locked') screen = sheet(v);
    else if (v.phase === 'night') screen = nightScreen(v);
    else screen = doneScreen(v);

    return h('div', { class: 'overlay' }, screen,
      S.update && v.phase !== 'free' ? h('div', { class: 'corner' }, updateCard()) : null,
      S.dialog === 'disable' ? disableDialog(v) : null,
      S.toast ? h('div', { class: 'toast' }, S.toast) : null);
  }

  function structureKey() {
    const v = S.view;
    return JSON.stringify([S.mode, S.dialog, S.disableMode, S.toast, S.warn > 0, DF.getLang(), S.busy, S.error,
      S.update && [S.update.version, S.update.busy, S.update.error],
      v && [v.phase, v.reason, v.test, v.active, v.timerRunning, v.paused, v.streak, v.openGroups,
        v.tasks.map((x) => [x.id, x.status, x.title, x.description, x.remainingSec > 0, x.note, x.launch])]]);
  }

  function patch() {
    const v = S.view;
    if (!v) return;
    for (const task of v.tasks) {
      const left = task.remainingSec;
      root.querySelectorAll('[data-ring="' + task.id + '"]').forEach((el) => {
        el.setAttribute('stroke-dashoffset', RING * (1 - progress(task)));
      });
      root.querySelectorAll('[data-left="' + task.id + '"]').forEach((el) => {
        if (left > 0 && !v.paused) el.textContent = t('timer.left', { t: clock(left) });
      });
      root.querySelectorAll('[data-done="' + task.id + '"]').forEach((el) => {
        if (left > 0) el.textContent = clock(left);
      });
    }
    root.querySelectorAll('[data-warn]').forEach((el) => { el.textContent = t('widget.warn.sub', { n: S.warn }); });
  }

  function render() {
    document.body.className = 'stage mode-' + S.mode;
    const key = structureKey();
    if (key === lastKey) { patch(); return; }
    lastKey = key;

    const focused = document.activeElement && document.activeElement.getAttribute('data-focus');
    const caret = focused ? document.activeElement.selectionStart : 0;
    root.replaceChildren(build());
    if (focused) {
      const el = root.querySelector('[data-focus="' + focused + '"]');
      if (el) { el.focus(); try { el.setSelectionRange(caret, caret); } catch (_) { /* не текстовое поле */ } }
    } else if (S.dialog === 'disable') {
      const el = root.querySelector('[data-focus="reason"]');
      if (el) el.focus();
    }
  }

  // ---------- события -----------------------------------------------------

  on('state', apply);
  on('mode', (mode) => { S.mode = mode; if (mode !== 'overlay') S.dialog = null; render(); });
  on('blocked', (b) => toast(b.reason === 'tabs' ? t('toast.tabs') : t('toast.app', { app: b.app })));
  on('tabWarning', (w) => { S.warn = w.seconds; render(); });
  on('askDisable', openDisable);
  on('update', (u) => { S.update = u; render(); });
  on('lang', (code) => { DF.setLang(code); render(); });
  on('theme', DF.setTheme);

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && S.dialog) { S.dialog = null; render(); }
  });

  setInterval(() => {
    if (S.view && S.view.phase === 'locked' && !S.busy) call('state').then(apply).catch(() => {});
  }, 1000);

  call('boot').then((b) => {
    S.boot = b;
    S.mode = b.mode || S.mode;
    S.update = b.update || null;
    DF.setTheme(b.theme);
    DF.setLang(b.lang);
    apply(b.view);
  });
})();
