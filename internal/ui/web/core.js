'use strict';

const DF = (() => {
  // ---------- мост -------------------------------------------------------
  const pending = new Map();
  const listeners = {};
  let seq = 0;

  function call(method, params) {
    return new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      window.chrome.webview.postMessage(JSON.stringify({ id, method, params: params || {} }));
    });
  }

  function on(event, fn) {
    (listeners[event] = listeners[event] || []).push(fn);
  }

  window.dofirst = {
    resolve(id, payload) {
      const p = pending.get(id);
      if (!p) return;
      pending.delete(id);
      if (payload && payload.error) p.reject(payload.error);
      else p.resolve(payload ? payload.result : undefined);
    },
    emit(event, data) {
      (listeners[event] || []).forEach((fn) => fn(data));
    },
  };

  function report(message) {
    try { call('client.error', { message: String(message).slice(0, 500) }).catch(() => {}); } catch (_) { /* моста нет */ }
  }
  window.addEventListener('error', (e) => report((e.message || 'error') + ' @' + (e.lineno || 0) + ':' + (e.colno || 0)));
  window.addEventListener('unhandledrejection', (e) => {
    const r = e.reason || {};
    report('rejection: ' + (r.stack || r.message || r.code || r));
  });

  // ---------- перевод ----------------------------------------------------
  let lang = 'ru';

  function setLang(code) {
    lang = window.DOFIRST_I18N[code] ? code : 'ru';
    document.documentElement.lang = lang;
  }

  function t(key, vars) {
    const dict = window.DOFIRST_I18N[lang];
    let s = dict[key] !== undefined ? dict[key] : window.DOFIRST_I18N.ru[key];
    if (s === undefined) return key;
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
    return s;
  }

  // plural выбирает форму слова
  function plural(n, key) {
    const forms = t(key).split('|');
    if (lang === 'ru') {
      const a = Math.abs(n) % 100, b = a % 10;
      if (a > 10 && a < 20) return forms[2];
      if (b === 1) return forms[0];
      if (b >= 2 && b <= 4) return forms[1];
      return forms[2];
    }
    return n === 1 ? forms[0] : forms[1];
  }

  // ---------- DOM --------------------------------------------------------

  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === null || v === undefined || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else if (k === 'value') el.value = v;
      else if (k === 'checked' || k === 'disabled' || k === 'selected') el[k] = true;
      else el.setAttribute(k, v === true ? '' : v);
    }
    let text = '';
    const flush = () => {
      if (!text) return;
      if (tag === 'button') {
        const label = document.createElement('span');
        label.className = 'lbl';
        label.textContent = text;
        el.append(label);
      } else {
        el.append(document.createTextNode(text));
      }
      text = '';
    };
    for (const kid of kids.flat(Infinity)) {
      if (kid === null || kid === undefined || kid === false) continue;
      if (kid.nodeType) { flush(); el.append(kid); } else text += String(kid);
    }
    flush();
    return el;
  }

  function dropdown(value, options, onchange) {
    const wrap = h('div', { class: 'dd' });
    const label = (v) => (options.find((o) => o.value === v) || options[0]).label;
    const button = h('button', { class: 'dd-btn', type: 'button', 'aria-haspopup': 'listbox' }, label(value), icon('down', 14));
    let menu = null;

    const close = () => {
      if (!menu) return;
      menu.remove();
      menu = null;
      wrap.classList.remove('open', 'up');
      document.removeEventListener('mousedown', outside, true);
      document.removeEventListener('keydown', keys, true);
    };
    const outside = (e) => { if (!wrap.contains(e.target)) close(); };
    const keys = (e) => {
      if (e.key === 'Escape') { e.stopPropagation(); close(); button.focus(); return; }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      const items = [...menu.children];
      const at = items.indexOf(document.activeElement);
      items[(at + (e.key === 'ArrowDown' ? 1 : items.length - 1)) % items.length].focus();
    };
    const pick = (option) => {
      close();
      button.focus();
      if (option.value === value) return;
      value = option.value;
      button.firstChild.textContent = option.label;
      onchange(value);
    };
    const open = () => {
      menu = h('div', { class: 'dd-menu', role: 'listbox' }, options.map((o) =>
        h('button', { class: 'dd-item' + (o.value === value ? ' on' : ''), type: 'button', role: 'option', onclick: () => pick(o) },
          o.label, o.value === value ? icon('check', 14) : null)));
      wrap.append(menu);
      wrap.classList.add('open');
      if (menu.getBoundingClientRect().bottom > window.innerHeight - 8) wrap.classList.add('up');
      document.addEventListener('mousedown', outside, true);
      document.addEventListener('keydown', keys, true);
      (menu.querySelector('.on') || menu.firstChild).focus();
    };
    button.addEventListener('click', () => (menu ? close() : open()));
    wrap.append(button);
    return wrap;
  }

  const ICONS = {
    check: '<path d="M4.5 12.5l5 5L19.5 6.5"/>',
    lock: '<rect x="5" y="10.5" width="14" height="9.5" rx="2.5"/><path d="M8.5 10.5V8a3.5 3.5 0 017 0v2.5"/>',
    arrow: '<path d="M5 12h14M13 6l6 6-6 6"/>',
    out: '<path d="M8 16L18 6M10 6h8v8"/>',
    power: '<path d="M12 3.5v8M6.8 6.8a7.4 7.4 0 1010.4 0"/>',
    pause: '<path d="M9 6v12M15 6v12"/>',
    play: '<path d="M8 5.5v13l10.5-6.5z"/>',
    moon: '<path d="M19.5 14.2A8 8 0 019.8 4.5a8 8 0 109.7 9.7z"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    edit: '<path d="M4.5 19.5h3.8L19 8.8 15.2 5 4.5 15.7v3.8zM13 7.2l3.8 3.8"/>',
    trash: '<path d="M5 7h14M10 7V4.5h4V7M7 7l1 12.5h8L17 7"/>',
    up: '<path d="M6 14l6-6 6 6"/>',
    down: '<path d="M6 10l6 6 6-6"/>',
    expand: '<path d="M9 4H4v5M15 20h5v-5M4 4l6 6M20 20l-6-6"/>',
    warn: '<path d="M12 4l9 15.5H3L12 4zM12 10v4.5M12 17.2v.3"/>',
    clock: '<circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/>',
    close: '<path d="M6 6l12 12M18 6L6 18"/>',
    minimize: '<path d="M5 12h14"/>',
    maximize: '<rect x="5.5" y="5.5" width="13" height="13" rx="1.5"/>',
    restore: '<rect x="5" y="8" width="11" height="11" rx="1.5"/><path d="M8.5 8V6.5A1.5 1.5 0 0110 5h7.5A1.5 1.5 0 0119 6.5V14a1.5 1.5 0 01-1.5 1.5H16"/>',
    settings: '<path d="M5 7h9M18 7h1M5 12h2M11 12h8M5 17h7M16 17h3"/><circle cx="16" cy="7" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="14" cy="17" r="2"/>',
  };

  function icon(name, size) {
    const span = document.createElement('span');
    span.className = 'ico';
    const px = size || 18;
    span.innerHTML = '<svg viewBox="0 0 24 24" width="' + px + '" height="' + px +
      '" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' +
      ICONS[name] + '</svg>';
    return span;
  }

  function logo(size) {
    const span = document.createElement('span');
    span.className = 'logo';
    span.innerHTML = '<svg viewBox="-6.2 0.5 108 108" width="' + size + '" height="' + size + '" fill="none" stroke-linecap="round" stroke-linejoin="round">' +
      '<path class="logo-box" stroke-width="8.5" d="M63 24H22a11 11 0 00-11 11v44a11 11 0 0011 11h44a11 11 0 0011-11V52"/>' +
      '<path class="logo-check" stroke-width="11.5" d="M31 57l14 14 46-56"/></svg>';
    return span;
  }

  // ---------- тема ---------------------------------------------------------

  const color = (() => {
    const rgb = (hex) => { const n = parseInt(String(hex).slice(1), 16) || 0; return [(n >> 16) & 255, (n >> 8) & 255, n & 255]; };
    const hex = (c) => '#' + c.map((x) => Math.round(Math.min(255, Math.max(0, x))).toString(16).padStart(2, '0')).join('');
    const valid = (s) => /^#[0-9a-f]{6}$/i.test(s);
    const mix = (a, b, k) => a.map((x, i) => x + (b[i] - x) * k);
    const lum = (c) => {
      const [r, g, b] = c.map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); });
      return 0.2126 * r + 0.7152 * g + 0.0722 * b;
    };
    const contrast = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
    const toward = (from, to, ok) => {
      for (let k = 0; k < 1; k += 0.04) { const c = mix(from, to, k); if (ok(c)) return c; }
      return to;
    };
    const fromHsv = (h, s, v) => {
      const f = (n) => { const k = (n + h / 60) % 6; return 255 * (v - v * s * Math.max(0, Math.min(k, 4 - k, 1))); };
      return [f(5), f(3), f(1)];
    };
    const toHsv = (c) => {
      const [r, g, b] = c.map((v) => v / 255);
      const max = Math.max(r, g, b), d = max - Math.min(r, g, b);
      let h = 0;
      if (d) {
        if (max === r) h = ((g - b) / d) % 6; else if (max === g) h = (b - r) / d + 2; else h = (r - g) / d + 4;
        h = (h * 60 + 360) % 360;
      }
      return { h, s: max ? d / max : 0, v: max };
    };
    return { rgb, hex, valid, mix, lum, contrast, toward, fromHsv, toHsv };
  })();

  const EXACT = {
    espresso: {
      bg: '#1a1209', surface: '#23190e', raised: '#2d2216', line: '#3e3122', text: '#f3ecdd', muted: '#b5a68e',
      accent: '#598175', 'accent-strong': '#6b978a', 'accent-text': '#9cc3b6', 'on-accent': '#f7f2e7',
      warn: '#e0ac62', danger: '#d98670', scrim: 'rgba(14, 9, 4, .72)',
    },
    cream: {
      bg: '#f4efe4', surface: '#fbf8f1', raised: '#ece5d6', line: '#d8cdb8', text: '#23190e', muted: '#6a5c49',
      accent: '#47695f', 'accent-strong': '#3c5b52', 'accent-text': '#3c5b52', 'on-accent': '#f7f2e7',
      warn: '#8a5a12', danger: '#a4442f', scrim: 'rgba(35, 25, 14, .4)',
    },
  };

  function palette(theme) {
    if (EXACT[theme.id]) return Object.assign({ dark: theme.id === 'espresso' }, EXACT[theme.id]);
    const { rgb, hex, mix, lum, contrast, toward } = color;
    const bg = rgb(theme.background), base = rgb(theme.accent), text = rgb(theme.text);
    const dark = lum(bg) < lum(text);
    const tone = contrast(base, bg) < 1.7;
    let surface, raised, accent;
    if (tone) {
      surface = mix(bg, base, 0.5);
      raised = base;
      accent = toward(base, text, (c) => contrast(c, surface) >= 4.5);
    } else {
      surface = dark ? mix(bg, text, 0.045) : mix(bg, [255, 255, 255], 0.55);
      if (contrast(surface, bg) < 1.03) surface = mix(bg, text, 0.035);
      raised = mix(bg, text, dark ? 0.11 : 0.08);
      accent = base;
    }
    const onAccent = contrast(accent, bg) >= contrast(accent, text) ? bg : text;
    const readable = (c) => contrast(c, surface) >= 4.5 && contrast(c, bg) >= 4.5;
    const edge = dark ? mix(bg, [0, 0, 0], 0.5) : text;

    return {
      dark,
      bg: hex(bg), surface: hex(surface), raised: hex(raised), text: hex(text),
      line: hex(mix(tone ? raised : bg, text, tone ? 0.14 : 0.17)),
      muted: hex(toward(mix(text, bg, 0.38), text, (c) => readable(c) && contrast(c, raised) >= 3.5)),
      accent: hex(accent),
      'accent-strong': hex(contrast(accent, text) < 1.5 ? mix(accent, bg, 0.2) : mix(accent, text, 0.14)),
      'accent-text': hex(toward(accent, text, readable)),
      'on-accent': hex(onAccent),
      warn: dark ? '#e0ac62' : '#8a5a12',
      danger: dark ? '#d98670' : '#a4442f',
      scrim: 'rgba(' + edge.map(Math.round).join(', ') + ', ' + (dark ? 0.72 : 0.4) + ')',
    };
  }

  function themeStyle(theme) {
    const p = palette(theme);
    return Object.keys(p).filter((k) => k !== 'dark').map((k) => '--' + k + ':' + p[k]).join(';') +
      ';color-scheme:' + (p.dark ? 'dark' : 'light');
  }

  let currentTheme = null;

  function setTheme(theme) {
    if (!theme || !color.valid(theme.background) || !color.valid(theme.accent) || !color.valid(theme.text)) return;
    currentTheme = theme;
    document.documentElement.setAttribute('style', themeStyle(theme));
  }

  setTheme(window.__dofirstTheme);

  function clock(sec) {
    sec = Math.max(0, Math.round(sec));
    const hh = Math.floor(sec / 3600), mm = Math.floor((sec % 3600) / 60), ss = sec % 60;
    const two = (n) => String(n).padStart(2, '0');
    return hh > 0 ? hh + ':' + two(mm) + ':' + two(ss) : two(mm) + ':' + two(ss);
  }

  function errorText(err) {
    const known = t('err.' + (err && err.code));
    if (known !== 'err.' + (err && err.code)) return known;
    return t('err.generic', { m: (err && err.message) || '' });
  }

  return {
    call, on, t, plural, setLang, getLang: () => lang, h, icon, logo, clock, errorText, dropdown,
    color, palette, themeStyle, setTheme, getTheme: () => currentTheme,
  };
})();
