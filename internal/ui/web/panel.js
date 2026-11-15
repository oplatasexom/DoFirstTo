'use strict';

(() => {
  if (window.__dofirstWindow !== 'panel') return;
  const { call, on, t, plural, h, icon, logo, errorText, dropdown } = DF;
  const root = document.getElementById('root');
  document.body.className = 'panel';

  const TABS = ['schedule', 'tasks', 'groups', 'journal', 'stats', 'themes', 'app'];
  const P = {
    boot: { version: '' },
    themes: [],           // готовые темы
    editor: null,         // окно своей темы: {id, name, background, accent, text, field, el}
    tab: 'schedule',
    settings: null,
    saved: '',            // JSON сохраненных настроек
    journal: null,
    stats: null,
    picker: null,         // окно "Добавить приложение" {groupId, tab, filter, query, exes, titles}
    apps: null,           // каталог, установленные и открытые программы
    appIndex: null,       // имя файла {name, icon}
    browsers: null,       // имена файлов браузеров
    flash: null,
    first: false,
  };
  let flashTimer = 0;

  // ---------- данные ------------------------------------------------------

  function normalize(s) {
    s.groups = s.groups || [];
    s.alwaysAllowed = s.alwaysAllowed || [];
    s.schedule.days = s.schedule.days || [];
    s.schedule.nightGroups = s.schedule.nightGroups || [];
    s.tasks.tasks = (s.tasks.tasks || []).map((task) => Object.assign({
      description: '', minMinutes: 0, requireNote: false, journal: false, needs: [], unlocks: [], launch: [], days: [],
    }, task));
    s.theme = s.theme || '';
    s.themes = s.themes || [];
    return s;
  }

  function themeById(id) {
    return P.settings.themes.find((x) => x.id === id) || P.themes.find((x) => x.id === id) || P.themes[0];
  }

  const dirty = () => P.settings && JSON.stringify(P.settings) !== P.saved;
  const newId = (prefix) => prefix + Date.now().toString(36) + Math.floor(Math.random() * 1296).toString(36);

  function flash(text, bad) {
    P.flash = { text, bad: !!bad };
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => { P.flash = null; render(); }, bad ? 6000 : 2500);
    render();
  }

  function loadSettings() {
    return call('settings.get').then((res) => {
      P.settings = normalize(res.settings);
      P.saved = JSON.stringify(P.settings);
      DF.setTheme(themeById(P.settings.theme));
    });
  }

  function validate() {
    const s = P.settings;
    for (const [i, task] of s.tasks.tasks.entries()) {
      if (!task.title.trim()) return t('v.title', { n: i + 1 });
      if (task.kind === 'timer' && !(task.minMinutes >= 1)) return t('v.minutes', { name: task.title });
    }
    for (const [i, g] of s.groups.entries()) {
      if (!g.name.trim()) return t('v.group.name', { n: i + 1 });
      if (!g.rule.split('|')[0].split(',').some((x) => x.trim())) return t('v.group.rule', { name: g.name });
    }
    if (s.schedule.enabled && !s.schedule.days.length) return t('v.days');
    return null;
  }

  function save() {
    const problem = validate();
    if (problem) { flash(problem, true); return; }
    call('settings.save', { settings: P.settings })
      .then((res) => {
        P.settings = normalize(res.settings);
        P.saved = JSON.stringify(P.settings);
        P.first = false;
        DF.setLang(P.settings.language);
        DF.setTheme(themeById(P.settings.theme));
        flash(t('saved'));
      })
      .catch((err) => flash(errorText(err), true));
  }

  function openTab(tab) {
    P.tab = tab;
    render();
    if (tab === 'journal') call('journal.list').then((list) => { P.journal = list || []; render(); }).catch(() => {});
    if (tab === 'stats') call('stats.get').then((st) => { P.stats = st; render(); }).catch(() => {});
    if (tab === 'groups' && !P.apps) {
      loadApps().then(() => {
        if (P.tab !== 'groups' || P.picker) return;
        for (const g of P.settings.groups) {
          const old = root.querySelector('[data-sum="' + g.id + '"]');
          if (old) old.replaceWith(groupSummary(g));
        }
      }).catch(() => {});
    }
  }

  // ---------- общие элементы ----------------------------------------------

  function touch() {
    const old = root.querySelector('.save');
    if (old) old.replaceWith(saveBar());
  }

  function saveBar() {
    const editable = ['schedule', 'tasks', 'groups', 'themes', 'app'].includes(P.tab);
    return h('div', { class: 'save' },
      editable && dirty() ? h('span', { class: 'hint dirty' }, t('dirty')) : null,
      editable ? h('button', { class: 'btn primary', disabled: !dirty(), onclick: save }, t('save')) : null);
  }

  function row(title, hint, control) {
    return h('div', { class: 'line-row' }, h('div', {}, h('b', {}, title), hint ? h('small', {}, hint) : null), control);
  }

  function toggle(value, onchange) {
    return h('label', { class: 'switch' },
      h('input', { type: 'checkbox', checked: !!value, onchange: (e) => onchange(e.target.checked) }), h('i'));
  }

  function text(value, oninput, attrs) {
    return h('input', Object.assign({ type: 'text', class: 'input', value: value || '', oninput: (e) => { oninput(e.target.value); touch(); } }, attrs || {}));
  }

  function number(value, min, max, onchange) {
    return h('input', {
      type: 'number', class: 'input num', min, max, value: String(value),
      oninput: (e) => { const n = parseInt(e.target.value, 10); onchange(Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : min); touch(); },
    });
  }

  function iconButton(name, onclick, disabled, extra) {
    return h('button', { class: 'icon-btn' + (extra ? ' ' + extra : ''), disabled, onclick }, icon(name, 17));
  }

  const WEEK = [1, 2, 3, 4, 5, 6, 0];
  const dayName = (d) => new Intl.DateTimeFormat(DF.getLang(), { weekday: 'short' }).format(new Date(2024, 0, 1 + ((d + 6) % 7)));

  // ---------- расписание ---------------------------------------------------

  function scheduleTab() {
    const sc = P.settings.schedule;
    const set = (fn) => { fn(); render(); };
    const time = (key) => h('input', { type: 'time', class: 'input time', value: sc[key], oninput: (e) => { if (e.target.value) sc[key] = e.target.value; touch(); } });

    const days = h('div', { class: 'days' }, WEEK.map((d) =>
      h('button', {
        class: 'pick day' + (sc.days.includes(d) ? ' on' : ''),
        onclick: () => set(() => { sc.days = sc.days.includes(d) ? sc.days.filter((x) => x !== d) : sc.days.concat(d); }),
      }, dayName(d))));

    return [
      P.first ? h('div', { class: 'card note-card' }, h('h3', {}, t('welcome.title')), h('p', {}, t('welcome.text'))) : null,
      h('div', { class: 'card' }, row(t('sch.enabled'), null, toggle(sc.enabled, (v) => set(() => { sc.enabled = v; })))),
      h('div', { class: 'card' },
        row(t('sch.bed'), t('sch.bed.hint'), time('bedtime')),
        row(t('sch.wake'), t('sch.wake.hint'), time('wakeTime')),
        row(t('sch.days'), t('sch.days.hint'), days)),
      h('div', { class: 'card' },
        row(t('sch.release'), t('sch.release.hint'), h('div', { class: 'kind' },
          sc.autoRelease ? time('autoRelease') : null,
          toggle(!!sc.autoRelease, (v) => set(() => { if (v) sc.autoRelease = '13:00'; else delete sc.autoRelease; })))),
        h('hr', { class: 'sep' }),
        row(t('sch.night'), t('sch.night.hint'), toggle(sc.nightScreen, (v) => set(() => { sc.nightScreen = v; }))),
        sc.nightScreen ? h('div', { class: 'label' },
          h('span', {}, t('sch.night.groups')),
          groupPicks(sc, 'nightGroups'),
          h('span', { class: 'help' }, t('sch.night.groups.hint'))) : null),
    ];
  }

  // ---------- задачи --------------------------------------------------------

  function groupPicks(task, field) {
    const groups = P.settings.groups;
    if (!groups.length) return h('span', { class: 'help' }, t('tasks.nogroups'));
    return h('div', { class: 'days' }, groups.map((g) =>
      h('button', {
        class: 'pick' + (task[field].includes(g.id) ? ' on' : ''),
        onclick: () => {
          task[field] = task[field].includes(g.id) ? task[field].filter((x) => x !== g.id) : task[field].concat(g.id);
          render();
        },
      }, g.name || '…')));
  }

  function taskDays(task) {
    const chosen = task.days.length ? task.days : WEEK;
    const sc = P.settings.schedule;
    const idle = sc.enabled && !chosen.some((d) => sc.days.includes(d));
    return h('div', { class: 'label full' },
      h('span', {}, t('tasks.days')),
      h('div', { class: 'days' }, WEEK.map((d) =>
        h('button', {
          class: 'pick day' + (chosen.includes(d) ? ' on' : ''),
          onclick: () => {
            const next = chosen.includes(d) ? chosen.filter((x) => x !== d) : chosen.concat(d);
            if (!next.length) return;
            task.days = next.length === WEEK.length ? [] : WEEK.filter((x) => next.includes(x));
            render();
          },
        }, dayName(d)))),
      h('span', { class: 'help' + (idle ? ' warn' : '') },
        idle ? t('tasks.days.idle') : task.days.length ? t('tasks.days.some') : t('tasks.days.every')));
  }

  function taskCard(task, i) {
    const list = P.settings.tasks.tasks;
    const move = (to) => { list.splice(to, 0, list.splice(i, 1)[0]); render(); };

    return h('div', { class: 'card task-card' },
      h('div', { class: 'task-top' },
        h('span', { class: 'task-num' }, i + 1),
        text(task.title, (v) => { task.title = v; }, { class: 'input title wide', placeholder: t('tasks.title.ph') }),
        h('div', { class: 'kind' },
          dropdown(task.kind, [{ value: 'check', label: t('tasks.kind.check') }, { value: 'timer', label: t('tasks.kind.timer') }],
            (v) => { task.kind = v; if (v === 'timer' && !task.minMinutes) task.minMinutes = 30; render(); }),
          task.kind === 'timer' ? number(task.minMinutes, 1, 600, (n) => { task.minMinutes = n; }) : null,
          task.kind === 'timer' ? h('span', { class: 'help' }, t('tasks.minutes')) : null),
        h('div', { class: 'tools' },
          iconButton('up', () => move(i - 1), i === 0),
          iconButton('down', () => move(i + 1), i === list.length - 1),
          iconButton('trash', () => { list.splice(i, 1); render(); }, false, 'del'))),
      h('div', { class: 'task-grid' },
        h('div', { class: 'full' }, text(task.description, (v) => { task.description = v; }, { class: 'input wide', placeholder: t('tasks.desc.ph') })),
        h('div', { class: 'checks full' },
          h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: task.requireNote, onchange: (e) => { task.requireNote = e.target.checked; touch(); } }), t('tasks.note')),
          h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: task.journal, onchange: (e) => { task.journal = e.target.checked; touch(); } }), t('tasks.journal'))),
        taskDays(task),
        h('div', { class: 'label' }, h('span', {}, t('tasks.needs')), groupPicks(task, 'needs')),
        h('div', { class: 'label' }, h('span', {}, t('tasks.unlocks')), groupPicks(task, 'unlocks')),
        h('label', { class: 'label full' }, h('span', {}, t('tasks.launch')),
          h('textarea', {
            class: 'input wide mono', rows: 2, placeholder: t('tasks.launch.ph'), value: task.launch.join('\n'),
            oninput: (e) => { task.launch = e.target.value.split('\n').map((x) => x.trim()).filter(Boolean); touch(); },
          }))));
  }

  function tasksTab() {
    const tl = P.settings.tasks;
    return [
      h('div', { class: 'card' }, row(t('tasks.strict'), t('tasks.strict.hint'), toggle(tl.strictOrder, (v) => { tl.strictOrder = v; render(); }))),
      tl.tasks.length ? tl.tasks.map(taskCard) : h('div', { class: 'empty' }, t('tasks.empty')),
      h('div', { class: 'actions' },
        h('button', { class: 'btn line', onclick: () => {
          tl.tasks.push({ id: newId('t'), title: '', description: '', kind: 'check', minMinutes: 0, requireNote: false, journal: false, needs: [], unlocks: [], launch: [], days: [] });
          render();
          const inputs = root.querySelectorAll('.input.title');
          if (inputs.length) inputs[inputs.length - 1].focus();
        } }, icon('plus', 16), t('tasks.add'))),
    ];
  }

  // ---------- группы программ ------------------------------------------------

  function removeGroup(id) {
    const s = P.settings;
    s.groups = s.groups.filter((g) => g.id !== id);
    s.schedule.nightGroups = s.schedule.nightGroups.filter((x) => x !== id);
    for (const task of s.tasks.tasks) {
      task.needs = task.needs.filter((x) => x !== id);
      task.unlocks = task.unlocks.filter((x) => x !== id);
    }
    render();
  }

  function normalizeExe(s) {
    s = s.trim().replace(/^["']+|["']+$/g, '').toLowerCase().split(/[\\/]/).pop().trim();
    if (!s) return '';
    return s.endsWith('.exe') ? s : s + '.exe';
  }

  function parseRule(rule) {
    const bar = rule.indexOf('|');
    const exes = new Set();
    const titles = new Map(); 
    (bar < 0 ? rule : rule.slice(0, bar)).split(',').forEach((part) => { const exe = normalizeExe(part); if (exe) exes.add(exe); });
    (bar < 0 ? '' : rule.slice(bar + 1)).split(',').forEach((part) => { const w = part.trim(); if (w) titles.set(w.toLowerCase(), w); });
    return { exes, titles };
  }

  function formatRule(exes, titles) {
    const left = [...exes].join(', ');
    return titles.size ? left + ' | ' + [...titles.values()].join(', ') : left;
  }

  function loadApps() {
    return call('apps.list').then((data) => {
      P.apps = data;
      P.appIndex = new Map();
      for (const p of data.open.concat(data.installed)) if (!P.appIndex.has(p.exe)) P.appIndex.set(p.exe, { name: p.name, icon: p.icon });
      // Название из каталога понятнее имени ярлыка, значок берём какой есть.
      for (const app of data.apps) for (const exe of app.exes) P.appIndex.set(exe, { name: app.name, icon: app.icon || (P.appIndex.get(exe) || {}).icon });
      P.browsers = new Set(data.apps.filter((a) => a.category === 'browsers').flatMap((a) => a.exes));
      return data;
    });
  }

  function appIcon(src, name) {
    return src ? h('img', { class: 'app-icon', src, alt: '' })
      : h('span', { class: 'app-icon letter' }, h('span', { class: 'lbl cap' }, (name || '?').slice(0, 1).toUpperCase()));
  }

  function groupSummary(g) {
    const { exes, titles } = parseRule(g.rule);
    const el = h('div', { class: 'group-sum', 'data-sum': g.id });
    if (!exes.size) { el.append(h('span', { class: 'help' }, t('groups.sum.empty'))); return el; }

    const seen = new Set();
    for (const exe of exes) {
      const known = P.appIndex && P.appIndex.get(exe);
      const name = known ? known.name : exe;
      if (seen.has(name)) continue; 
      seen.add(name);
      el.append(h('span', { class: 'app-chip' }, appIcon(known && known.icon, name), name));
    }
    const hasBrowser = P.browsers && [...exes].some((e) => P.browsers.has(e));
    if (titles.size) {
      el.append(h('span', { class: 'chip-label' }, t('groups.sum.sites')), ...[...titles.values()].map((w) => h('span', { class: 'chip on' }, w)));
      if (P.browsers && !hasBrowser) el.append(h('span', { class: 'help warn' }, t('picker.nobrowser')));
    } else if (hasBrowser) {
      el.append(h('span', { class: 'help' }, t('groups.sum.whole')));
    }
    return el;
  }

  function groupsTab() {
    const groups = P.settings.groups;
    const refresh = (g) => {
      const old = root.querySelector('[data-sum="' + g.id + '"]');
      if (old) old.replaceWith(groupSummary(g));
    };
    return [
      h('div', { class: 'card' }, h('p', { class: 'help' }, t('groups.help')), h('div', {}, h('code', { class: 'example' }, t('groups.example')))),
      groups.length
        ? groups.map((g) => h('div', { class: 'card group-card' },
          h('div', { class: 'group-top' },
            text(g.name, (v) => { g.name = v; }, { class: 'input title wide', placeholder: t('groups.name.ph') }),
            iconButton('trash', () => removeGroup(g.id), false, 'del')),
          h('div', { class: 'group-rule' },
            text(g.rule, (v) => { g.rule = v; refresh(g); }, { class: 'input mono wide', placeholder: t('groups.rule.ph'), spellcheck: 'false' }),
            h('button', { class: 'btn line', onclick: () => openPicker(g) }, icon('plus', 16), t('groups.pick'))),
          groupSummary(g)))
        : h('div', { class: 'empty' }, t('groups.empty')),
      h('div', { class: 'actions' },
        h('button', { class: 'btn line', onclick: () => {
          groups.push({ id: newId('g'), name: '', rule: '' });
          render();
          const inputs = root.querySelectorAll('.group-card .input.title');
          if (inputs.length) inputs[inputs.length - 1].focus();
        } }, icon('plus', 16), t('groups.add'))),
    ];
  }

  // ---------- окно «Добавить приложение» --------------------------------------

  function openPicker(group) {
    P.picker = Object.assign({ groupId: group.id, tab: 'catalog', filter: 'all', query: '', custom: '' }, parseRule(group.rule));
    render();
    loadApps().then(() => { if (P.picker) render(); }).catch((err) => { P.picker = null; flash(errorText(err), true); });
  }

  function pickerItems() {
    const pk = P.picker, data = P.apps;
    const app = (name, icon, sub, exes, category, dim) => ({
      name, icon, sub, category, dim,
      on: exes.some((e) => pk.exes.has(e)),
      set: (v) => exes.forEach((e) => (v ? pk.exes.add(e) : pk.exes.delete(e))),
    });
    switch (pk.tab) {
      case 'catalog':
        return data.apps.slice().sort((a, b) => Number(b.installed) - Number(a.installed)).map((a) =>
          app(a.name, a.icon, a.installed ? a.exes.join(', ') : t('picker.missing'), a.exes, a.category, !a.installed));
      case 'installed':
        return data.installed.map((p) => app(p.name, p.icon, p.exe, [p.exe], p.category));
      case 'open':
        return data.open.map((p) => app(p.name, p.icon, p.title, [p.exe], p.category));
      default:
        return data.sites.map((s) => ({
          name: s.name, sub: s.titles.join(', '), category: s.category,
          on: s.titles.some((w) => pk.titles.has(w.toLowerCase())),
          set: (v) => s.titles.forEach((w) => (v ? pk.titles.set(w.toLowerCase(), w) : pk.titles.delete(w.toLowerCase()))),
        }));
    }
  }

  function pickerBody() {
    const pk = P.picker;
    if (!P.apps) return h('div', { class: 'pick-body' }, h('div', { class: 'empty' }, t('picker.loading')));

    const all = pickerItems();
    const cats = P.apps.categories.filter((c) => all.some((it) => it.category === c));
    const query = pk.query.trim().toLowerCase();
    const shown = all.filter((it) => (pk.filter === 'all' || it.category === pk.filter) &&
      (!query || it.name.toLowerCase().includes(query) || String(it.sub).toLowerCase().includes(query)));

    const filter = (id, label) => h('button', { class: 'pick' + (pk.filter === id ? ' on' : ''), onclick: () => { pk.filter = id; refreshPicker(); } }, label);
    const body = h('div', { class: 'pick-body' },
      cats.length > 1 ? h('div', { class: 'days' }, filter('all', t('picker.all')), cats.map((c) => filter(c, t('cat.' + c)))) : null,
      h('p', { class: 'help' }, t('picker.hint.' + pk.tab)),
      shown.length ? h('div', { class: 'pick-list' }, shown.map((it) =>
        h('label', { class: 'pick-item' + (it.dim ? ' dim' : '') },
          appIcon(it.icon, it.name),
          h('span', { class: 'pick-text' }, h('b', {}, it.name), h('small', {}, it.sub)),
          h('span', { class: 'switch' }, h('input', { type: 'checkbox', checked: it.on, onchange: (e) => { it.set(e.target.checked); refreshPickerFoot(); } }), h('i')))))
        : h('div', { class: 'empty' }, t('picker.empty')));

    if (pk.tab === 'sites') {
      const known = new Set(P.apps.sites.flatMap((s) => s.titles.map((w) => w.toLowerCase())));
      const own = [...pk.titles.entries()].filter(([key]) => !known.has(key));
      const add = () => {
        const w = pk.custom.trim().replace(/[,|]/g, ' ');
        if (w) pk.titles.set(w.toLowerCase(), w);
        pk.custom = '';
        refreshPicker();
      };
      body.append(h('div', { class: 'pick-custom' },
        h('input', { type: 'text', class: 'input', placeholder: t('picker.custom'), value: pk.custom, 'data-keep': 'custom',
          oninput: (e) => { pk.custom = e.target.value; }, onkeydown: (e) => { if (e.key === 'Enter') add(); } }),
        h('button', { class: 'btn line', onclick: add }, t('picker.custom.add')),
        own.map(([key, w]) => h('button', { class: 'chip on removable', onclick: () => { pk.titles.delete(key); refreshPicker(); } }, w, icon('close', 12)))));
    }
    return body;
  }

  function pickerFoot() {
    const pk = P.picker;
    const hasBrowser = P.browsers && [...pk.exes].some((e) => P.browsers.has(e));
    let hint = null;
    if (pk.titles.size && P.browsers && !hasBrowser) hint = h('span', { class: 'help warn' }, t('picker.nobrowser'));
    else if (hasBrowser && !pk.titles.size) hint = h('span', { class: 'help' }, t('picker.wholebrowser'));

    return h('div', { class: 'pick-foot' },
      h('div', { class: 'pick-status' }, h('b', {}, t('picker.summary', { apps: pk.exes.size, sites: pk.titles.size })), hint),
      h('div', { class: 'dialog-actions' },
        h('button', { class: 'btn quiet', onclick: () => { P.picker = null; render(); } }, t('cancel')),
        h('button', { class: 'btn primary', onclick: () => {
          const group = P.settings.groups.find((g) => g.id === pk.groupId);
          if (group) group.rule = formatRule(pk.exes, pk.titles);
          P.picker = null;
          render();
        } }, t('picker.save'))));
  }

  function refreshPickerFoot() {
    const old = root.querySelector('.pick-foot');
    if (old) old.replaceWith(pickerFoot());
  }

  function refreshPicker() {
    const keep = document.activeElement && document.activeElement.getAttribute('data-keep');
    const old = root.querySelector('.pick-body');
    if (old) old.replaceWith(pickerBody());
    refreshPickerFoot();
    if (keep) {
      const el = root.querySelector('[data-keep="' + keep + '"]');
      if (el) el.focus();
    }
  }

  function pickerDialog() {
    const pk = P.picker;
    const group = P.settings.groups.find((g) => g.id === pk.groupId);
    const tab = (id) => h('button', { class: pk.tab === id ? 'on' : '', onclick: () => { pk.tab = id; pk.filter = 'all'; refreshPicker(); markTab(); } }, t('picker.tab.' + id));
    const markTab = () => root.querySelectorAll('.pick-tabs button').forEach((b, i) => b.classList.toggle('on', ['catalog', 'installed', 'open', 'sites'][i] === pk.tab));

    return h('div', { class: 'scrim', onclick: (e) => { if (e.target.classList.contains('scrim')) { P.picker = null; render(); } } },
      h('div', { class: 'dialog picker' },
        h('div', { class: 'pick-head' },
          h('h2', {}, t('picker.title'), group && group.name ? h('span', {}, ' · ', group.name) : null),
          h('input', { type: 'search', class: 'input', placeholder: t('picker.search'), value: pk.query, oninput: (e) => { pk.query = e.target.value; refreshPicker(); } })),
        h('div', { class: 'pick-tabs' }, ['catalog', 'installed', 'open', 'sites'].map(tab)),
        pickerBody(),
        pickerFoot()));
  }

  // ---------- журнал и статистика ---------------------------------------------

  const dateFmt = (iso, opts) => new Intl.DateTimeFormat(DF.getLang(), opts).format(new Date(iso));

  function journalTab() {
    if (!P.journal) return [];
    if (!P.journal.length) return [h('div', { class: 'empty' }, t('journal.empty'))];
    const list = P.journal.slice().sort((a, b) => (a.at < b.at ? 1 : -1));
    return [h('div', { class: 'card' }, list.map((e) => h('div', { class: 'entry' },
      h('time', {}, dateFmt(e.at, { day: 'numeric', month: 'short' })),
      h('div', {}, h('b', {}, e.title), e.minutes ? h('span', { class: 'help' }, ' · ', e.minutes, ' ', t('min')) : null),
      e.note ? h('p', {}, e.note) : null)))];
  }

  function statsTab() {
    const st = P.stats;
    if (!st) return [];
    if (!st.days.length && !st.disables.length) return [h('div', { class: 'empty' }, t('stats.empty'))];

    const tile = (label, value, unit) => h('div', { class: 'tile' }, h('span', {}, label), h('b', {}, value, unit ? h('small', {}, ' ', unit) : null));
    const days = st.days.slice(-30);
    const peak = Math.max(1, ...days.map((d) => d.minutes));

    return [
      h('div', { class: 'tiles' },
        tile(t('stats.streak'), st.streak, plural(st.streak, 'days')),
        tile(t('stats.best'), st.best, plural(st.best, 'days')),
        tile(t('stats.done'), st.doneDays),
        tile(t('stats.avg'), st.avgRoutineMin, t('min'))),
      days.length ? h('div', { class: 'card' }, h('h3', {}, t('stats.days')),
        h('div', { class: 'bars' }, days.map((d) => h('div', {
          class: 'bar' + (d.done ? '' : ' open'),
          style: 'height:' + (d.done ? Math.max(6, Math.round(100 * d.minutes / peak)) : 6) + '%',
          title: d.cycle + ' · ' + (d.done ? d.minutes + ' ' + t('min') : t('stats.open')),
        }))),
        h('div', { class: 'bar-axis' }, h('span', {}, days[0].cycle), h('span', {}, days[days.length - 1].cycle))) : null,
      st.disables.length ? h('div', { class: 'card' }, h('h3', {}, t('stats.disables')),
        st.disables.slice().reverse().map((d) => h('div', { class: 'entry' },
          h('time', {}, dateFmt(d.at, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })),
          h('div', {}, h('b', {}, t('disable.' + d.mode)), h('span', { class: 'help' }, ' · ', d.reason || t('stats.noreason')))))) : null,
    ];
  }

  // ---------- темы --------------------------------------------------------------

  const builtin = (theme) => P.themes.some((x) => x.id === theme.id);
  const themeName = (theme) => (builtin(theme) ? t('theme.' + theme.id) : theme.name || t('themes.newname'));

  function selectTheme(theme) {
    P.settings.theme = theme.id;
    DF.setTheme(theme);
    render();
  }

  function removeTheme(id) {
    const s = P.settings;
    s.themes = s.themes.filter((x) => x.id !== id);
    if (s.theme === id) s.theme = '';
    DF.setTheme(themeById(s.theme));
    render();
  }

  // образец темыб маленькое окно приложения в ее цветах
  function themeItem(theme, own) {
    const on = themeById(P.settings.theme).id === theme.id;
    return h('div', { class: 'theme-item' },
      h('button', { class: 'theme-pick' + (on ? ' on' : ''), title: themeName(theme), onclick: () => selectTheme(theme) },
        h('span', { class: 'theme-card', style: DF.themeStyle(theme) },
          h('span', { class: 'tc-nav' }, h('i'), h('i', { class: 'on' }), h('i')),
          h('span', { class: 'tc-body' }, h('b', {}, 'Aa'), h('i'), h('i', { class: 'short' }), h('i', { class: 'btn-dot' }))),
        on ? h('span', { class: 'theme-mark' }, icon('check', 13)) : null),
      own ? h('div', { class: 'theme-tools' },
        iconButton('edit', () => openEditor(theme)),
        iconButton('trash', () => removeTheme(theme.id), false, 'del')) : null,
      h('div', { class: 'theme-name' }, h('span', {}, themeName(theme))));
  }

  function themesTab() {
    const section = (title, hint, items) => h('div', { class: 'card' },
      h('h3', {}, title), hint ? h('p', { class: 'help' }, hint) : null, h('div', { class: 'theme-grid' }, items));
    const dark = P.themes.filter((x) => DF.palette(x).dark);
    const light = P.themes.filter((x) => !DF.palette(x).dark);
    return [
      section(t('themes.dark'), null, dark.map((x) => themeItem(x))),
      section(t('themes.light'), null, light.map((x) => themeItem(x))),
      section(t('themes.own'), t('themes.own.hint'), [
        P.settings.themes.map((x) => themeItem(x, true)),
        h('div', { class: 'theme-item' },
          h('button', { class: 'theme-pick add', title: t('themes.create'), onclick: () => openEditor(null) },
            h('span', { class: 'theme-add' }, icon('plus', 26))),
          h('div', { class: 'theme-name' }, h('span', {}, t('themes.create')))),
      ]),
    ];
  }

  // ---------- окно своей темы ----------------------------------------------------

  const COLOR_FIELDS = ['background', 'accent', 'text'];

  function openEditor(theme) {
    const base = theme || themeById(P.settings.theme);
    const ed = {
      id: theme ? theme.id : '', name: theme ? theme.name : t('themes.newname'),
      background: base.background, accent: base.accent, text: base.text, field: 'accent',
    };
    ed.el = editorDialog(ed);
    P.editor = ed;
    render();
  }

  function closeEditor() {
    P.editor = null;
    render();
  }

  function editorDialog(ed) {
    const { color } = DF;
    const clamp = (x) => Math.min(1, Math.max(0, x));
    let hsv = color.toHsv(color.rgb(ed[ed.field]));

    const dot = h('i', { class: 'cp-dot' });
    const knob = h('i', { class: 'cp-knob' });
    const square = h('div', { class: 'cp-sv' }, dot);
    const hue = h('div', { class: 'cp-hue' }, knob);
    const note = h('p', { class: 'help' });
    const apply = h('button', { class: 'btn primary', onclick: done }, t('themes.apply'));
    const name = h('input', { type: 'text', class: 'input wide', maxlength: 40, value: ed.name, oninput: (e) => { ed.name = e.target.value; } });

    const preview = h('div', { class: 'theme-preview' },
      h('div', { class: 'tp-nav' }, logo(24),
        h('span', { class: 'tp-tab on' }, t('nav.tasks')),
        h('span', { class: 'tp-tab' }, t('nav.journal')),
        h('span', { class: 'tp-tab' }, t('nav.stats'))),
      h('div', { class: 'tp-main' },
        h('p', { class: 'motto' }, t('motto')),
        h('h3', {}, t('themes.sample.title')),
        h('div', { class: 'card' },
          h('label', { class: 'check' }, h('input', { type: 'checkbox', checked: true, tabindex: '-1' }), t('tasks.note')),
          h('div', { class: 'chips' }, h('span', { class: 'chip on' }, t('cat.study')), h('span', { class: 'chip' }, t('cat.music'))),
          h('p', { class: 'help' }, t('themes.sample'))),
        h('div', { class: 'actions' },
          h('button', { class: 'btn primary', tabindex: '-1' }, icon('check', 16), t('task.done')),
          h('button', { class: 'btn line', tabindex: '-1' }, t('cancel')))));

    const rows = {};
    for (const field of COLOR_FIELDS) {
      const swatch = h('span', { class: 'te-swatch' });
      const input = h('input', {
        type: 'text', class: 'input mono', maxlength: 7, spellcheck: 'false', value: ed[field],
        onfocus: () => choose(field),
        oninput: (e) => {
          let v = e.target.value.trim().toLowerCase();
          if (v && v[0] !== '#') v = '#' + v;
          if (!color.valid(v)) return;
          ed[field] = v;
          hsv = color.toHsv(color.rgb(v));
          refresh(input);
        },
        onblur: () => { input.value = ed[field]; },
      });
      rows[field] = { swatch, input, row: h('div', { class: 'te-color', onmousedown: () => choose(field) }, swatch, h('b', {}, t('themes.' + field)), input) };
    }

    function choose(field) {
      if (ed.field === field) return;
      ed.field = field;
      hsv = color.toHsv(color.rgb(ed[field]));
      refresh();
    }
    
    function refresh(skip) {
      const pure = color.hex(color.fromHsv(hsv.h, 1, 1));
      for (const field of COLOR_FIELDS) {
        rows[field].row.classList.toggle('on', ed.field === field);
        rows[field].swatch.style.background = ed[field];
        if (rows[field].input !== skip) rows[field].input.value = ed[field];
      }
      square.style.background = 'linear-gradient(to top, #000, transparent), linear-gradient(to right, #fff, ' + pure + ')';
      dot.style.left = hsv.s * 100 + '%';
      dot.style.top = (1 - hsv.v) * 100 + '%';
      dot.style.background = ed[ed.field];
      knob.style.left = (hsv.h / 360) * 100 + '%';
      knob.style.background = pure;
      preview.setAttribute('style', DF.themeStyle({ id: 'draft', background: ed.background, accent: ed.accent, text: ed.text }));

      const legible = color.contrast(color.rgb(ed.text), color.rgb(ed.background));
      note.className = legible < 3 ? 'error' : legible < 4.5 ? 'help warn' : 'help';
      note.textContent = legible < 3 ? t('themes.unreadable') : legible < 4.5 ? t('themes.lowcontrast') : t('themes.sample.hint');
      apply.disabled = legible < 3;
    }

    function drag(el, move) {
      el.addEventListener('pointerdown', (e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        el.setPointerCapture(e.pointerId);
        const at = (ev) => {
          const r = el.getBoundingClientRect();
          move(clamp((ev.clientX - r.left) / r.width), clamp((ev.clientY - r.top) / r.height));
          ed[ed.field] = color.hex(color.fromHsv(hsv.h, hsv.s, hsv.v));
          refresh();
        };
        const stop = () => {
          el.removeEventListener('pointermove', at);
          el.removeEventListener('pointerup', stop);
          el.removeEventListener('pointercancel', stop);
        };
        el.addEventListener('pointermove', at);
        el.addEventListener('pointerup', stop);
        el.addEventListener('pointercancel', stop);
        at(e);
      });
    }
    drag(square, (x, y) => { hsv.s = x; hsv.v = 1 - y; });
    drag(hue, (x) => { hsv.h = Math.min(359.9, x * 360); });

    function done() {
      const list = P.settings.themes;
      const theme = {
        id: ed.id || newId('th'), name: ed.name.trim() || t('themes.newname'),
        background: ed.background, accent: ed.accent, text: ed.text,
      };
      const at = list.findIndex((x) => x.id === theme.id);
      if (at >= 0) list[at] = theme; else list.push(theme);
      P.editor = null;
      selectTheme(theme);
    }

    const scrim = h('div', { class: 'scrim', onmousedown: (e) => { if (e.target === scrim) closeEditor(); } },
      h('div', { class: 'dialog theme-editor' },
        h('h2', {}, t('themes.edit')),
        h('div', { class: 'te-grid' },
          h('div', { class: 'te-left' },
            h('label', { class: 'label' }, h('span', {}, t('themes.name')), name),
            h('div', { class: 'te-colors' }, COLOR_FIELDS.map((field) => rows[field].row)),
            h('div', { class: 'cp' }, square, hue)),
          h('div', { class: 'te-right' }, preview, note)),
        h('div', { class: 'dialog-actions' },
          h('button', { class: 'btn quiet', onclick: closeEditor }, t('cancel')), apply)));
    refresh();
    return scrim;
  }

  // ---------- приложение ------------------------------------------------------

  function appTab() {
    const s = P.settings;
    return [
      h('div', { class: 'card' },
        row(t('app.lang'), null, dropdown(s.language, [{ value: 'ru', label: 'Русский' }, { value: 'en', label: 'English' }],
          (v) => { s.language = v; touch(); })),
        row(t('app.autostart'), null, toggle(s.autostart, (v) => { s.autostart = v; touch(); })),
        row(t('app.hotkey'), t('app.hotkey.hint'), text(s.hotkey, (v) => { s.hotkey = v; }, { class: 'input mono', spellcheck: 'false' }))),
      h('div', { class: 'card' },
        row(t('app.countdown'), null, number(s.tabCountdownSec, 0, 120, (n) => { s.tabCountdownSec = n; })),
        h('label', { class: 'label' }, h('span', {}, t('app.always')),
          text(s.alwaysAllowed.join(', '), (v) => { s.alwaysAllowed = v.split(',').map((x) => x.trim()).filter(Boolean); }, { class: 'input wide mono', spellcheck: 'false' }),
          h('span', { class: 'help' }, t('app.always.hint')))),
      h('div', { class: 'card' },
        row(t('app.test'), t('app.test.hint'), h('button', { class: 'btn line', onclick: () => call('lock.test').catch((err) => flash(errorText(err), true)) }, t('app.test'))),
        h('hr', { class: 'sep' }),
        row(t('app.version', { v: P.boot.version }), null, h('button', { class: 'btn line', onclick: () => {
          call('update.check').then((u) => {
            if (!u.configured) flash(t('app.update.off'));
            else if (!u.version) flash(t('app.update.none'));
          }).catch((err) => flash(errorText(err), true));
        } }, t('app.update.check')))),
    ];
  }

  // ---------- верхняя панель окна ---------------------------------------------

  function dragWindow(e) {
    if (e.button !== 0 || e.target.closest('button')) return;
    if (e.detail === 2) call('window.maximize').catch(() => {});
    else call('window.drag', { edge: e.target.classList.contains('tb-grip') ? 'top' : '' }).catch(() => {});
  }

  function titlebar() {
    const button = (name, action, extra) =>
      h('button', { class: 'tb-btn' + (extra || ''), tabindex: '-1', onclick: () => call(action).catch(() => {}) }, icon(name, 15));
    return h('header', { class: 'titlebar', onmousedown: dragWindow },
      h('div', { class: 'tb-grip' }),
      button('minimize', 'window.minimize'),
      button(P.maximized ? 'restore' : 'maximize', 'window.maximize'),
      button('close', 'window.close', ' close'));
  }

  // ---------- отрисовка -------------------------------------------------------
  function render() {
    if (!P.settings) { root.replaceChildren(); return; }

    const body = { schedule: scheduleTab, tasks: tasksTab, groups: groupsTab, journal: journalTab, stats: statsTab, themes: themesTab, app: appTab }[P.tab]();
    const scroll = root.querySelector('.page-body');
    const top = scroll ? scroll.scrollTop : 0;

    root.replaceChildren(h('div', { class: 'panel-wrap' },
      titlebar(),
      h('nav', { class: 'nav' },
        h('div', { class: 'brand', onmousedown: dragWindow }, logo(28), h('span', {}, 'DoFirstTo')),
        TABS.map((tab) => h('button', { class: tab === P.tab ? 'on' : '', onclick: () => openTab(tab) }, t('nav.' + tab))),
        h('span', { class: 'version' }, t('app.version', { v: P.boot.version }))),
      h('section', { class: 'page' },
        h('header', { class: 'page-head' }, h('h1', {}, t('nav.' + P.tab)), saveBar()),
        h('div', { class: 'page-body' }, body)),
      P.picker ? pickerDialog() : null,
      P.editor ? P.editor.el : null,
      P.flash ? h('div', { class: 'flash' + (P.flash.bad ? ' bad' : '') }, P.flash.text) : null));

    const next = root.querySelector('.page-body');
    if (next) next.scrollTop = top;
  }

  // ---------- события ---------------------------------------------------------

  on('lang', (code) => { DF.setLang(code); render(); });
  on('theme', (theme) => { DF.setTheme(theme); if (P.settings) render(); });
  on('window', (state) => {
    P.maximized = !!state.maximized;
    const old = root.querySelector('.titlebar');
    if (old) old.replaceWith(titlebar());
  });
  on('tab', (tab) => { if (TABS.includes(tab)) openTab(tab); });
  on('shown', () => {
    if (dirty()) { render(); return; }
    loadSettings().then(() => openTab(P.tab)).catch(() => {});
  });

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && P.editor) closeEditor();
    if (e.key === 'Escape' && P.picker) { P.picker = null; render(); }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's' && dirty()) { e.preventDefault(); save(); }
  });

  call('boot').then((b) => {
    P.boot = b;
    P.themes = b.themes || [];
    DF.setTheme(b.theme);
    P.first = !!b.firstLaunch;
    P.tab = TABS.includes(b.tab) ? b.tab : 'schedule';
    DF.setLang(b.lang);
    return loadSettings();
  }).then(() => openTab(P.tab));
})();
