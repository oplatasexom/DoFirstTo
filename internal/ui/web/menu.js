'use strict';

(() => {
  if (window.__dofirstWindow !== 'menu') return;
  const { call, on, t, plural, h, icon, logo, clock } = DF;
  const root = document.getElementById('root');
  document.body.className = 'menu';

  let view = null;

  function status(v) {
    if (v.phase === 'night') return { title: t('menu.night'), sub: '' };
    if (v.phase === 'locked') {
      const task = v.tasks.find((x) => x.id === v.active);
      if (!task) return { title: t('menu.pick'), sub: '' };
      let sub = t('widget.check');
      if (task.kind === 'timer') {
        sub = task.remainingSec <= 0 ? t('timer.ready')
          : v.paused ? t('timer.paused') : t('timer.left', { t: clock(task.remainingSec) });
      }
      return { title: task.title, sub };
    }
    const streak = v.streak > 0 ? t('streak') + ' · ' + v.streak + ' ' + plural(v.streak, 'days') : '';
    if (v.reason === 'done') return { title: t('menu.done'), sub: streak };
    if (v.reason === 'armed' || v.reason === 'snoozed') {
      const time = new Intl.DateTimeFormat(DF.getLang(), { hour: '2-digit', minute: '2-digit' }).format(new Date(v.next));
      return { title: t('menu.free'), sub: t('menu.armed', { time }) };
    }
    return { title: t('menu.free'), sub: streak };
  }

  function render() {
    if (!view) return;
    const v = view;
    const locked = v.phase === 'locked' || v.phase === 'night';
    const task = v.tasks.find((x) => x.id === v.active);
    const timing = v.phase === 'locked' && task && task.kind === 'timer' && task.remainingSec > 0;
    const st = status(v);

    const item = (id, name, label, extra) =>
      h('button', { class: 'menu-item' + (extra || ''), onclick: () => call('menu.act', { id }).catch(() => {}) }, icon(name, 17), label);

    root.replaceChildren(h('div', { class: 'menu-wrap' },
      h('div', { class: 'menu-head' }, logo(30),
        h('div', { class: 'menu-status' }, h('b', {}, st.title), st.sub ? h('small', {}, st.sub) : null)),
      h('div', { class: 'menu-items' },
        locked ? item('show', 'expand', t('menu.show')) : null,
        timing ? item(v.paused ? 'resume' : 'pause', v.paused ? 'play' : 'pause', v.paused ? t('timer.resume') : t('timer.pause')) : null,
        item('settings', 'settings', t('menu.settings')),
        locked ? item('disable', 'power', t('menu.disable')) : null,
        h('hr', { class: 'sep' }),
        item('quit', 'out', t('menu.quit'), ' quiet'))));

    const height = Math.ceil(root.firstChild.getBoundingClientRect().height * window.devicePixelRatio);
    call('menu.size', { height }).catch(() => {});
  }

  on('state', (v) => { view = v; render(); });
  on('lang', (code) => { DF.setLang(code); render(); });
  on('theme', DF.setTheme);
  document.addEventListener('keydown', (e) => { if (e.key === 'Escape') call('menu.act', { id: 'close' }).catch(() => {}); });

  call('boot').then((b) => {
    DF.setTheme(b.theme);
    DF.setLang(b.lang);
    view = b.view;
    render();
  });
})();
