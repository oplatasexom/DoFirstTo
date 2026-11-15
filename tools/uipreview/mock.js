'use strict';

(() => {
  const q = new URLSearchParams(location.search);
  const scene = q.get('scene') || 'tasks';
  const lang = q.get('lang') || 'ru';
  const now = new Date();

  const groups = [
    { id: 'study-web', name: 'Учёба в браузере', rule: 'chrome.exe, msedge.exe, firefox.exe, zen.exe | Claude, roadmap.sh, .pdf' },
    { id: 'ide', name: 'Редактор кода', rule: 'code.exe, goland64.exe' },
    { id: 'music', name: 'Музыка', rule: 'spotify.exe' },
  ];

  const settings = {
    language: lang,
    schedule: { enabled: true, bedtime: '23:00', wakeTime: '07:00', days: [1, 2, 3, 4, 5, 6, 0], nightScreen: scene === 'nightset', nightGroups: ['music'] },
    tasks: {
      strictOrder: scene !== 'free',
      tasks: [
        { id: 'teeth', title: 'Почистить зубы', kind: 'check', unlocks: ['music'] },
        { id: 'study', title: 'Учиться по roadmap', description: 'Раздел про конкурентность: каналы, select, контексты.', kind: 'timer', minMinutes: 60, requireNote: true, journal: true, needs: ['study-web', 'ide'], launch: ['https://roadmap.sh/golang', 'D:\\Books\\concurrency-in-go.pdf'] },
        { id: 'plan', title: 'План на день', description: 'Три главных дела — в заметки.', kind: 'check', days: [1, 3, 5] },
      ],
    },
    groups,
    alwaysAllowed: ['obsidian.exe'],
    tabCountdownSec: 5,
    autostart: true,
    hotkey: 'Ctrl+Alt+D',
    theme: q.get('theme') || '',
    themes: [{ id: 'own', name: 'Ночной океан', background: '#0b1622', accent: '#2ec4b6', text: '#e8f1f2' }],
  };

  const builtinThemes = window.__dofirstThemes || [];
  const activeTheme = () => settings.themes.concat(builtinThemes).find((x) => x.id === settings.theme) || builtinThemes[0];
  window.__dofirstTheme = activeTheme();

  let remaining = 42 * 60 + 7;
  let paused = scene === 'paused';
  const names = (ids) => (ids || []).map((id) => groups.find((g) => g.id === id).name);

  function view() {
    const free = scene === 'free';
    const tasks = settings.tasks.tasks.map((task, i) => ({
      id: task.id, title: task.title, description: task.description || '', kind: task.kind,
      minMinutes: task.minMinutes || 0, requireNote: !!task.requireNote,
      status: free ? (i === 1 ? 'active' : 'ready') : ['done', 'active', 'locked'][i],
      elapsedSec: i === 1 ? 3600 - remaining : 0, remainingSec: i === 1 ? remaining : 0,
      note: '', needs: names(task.needs), unlocks: names(task.unlocks), launch: task.launch || [],
    }));
    const next = new Date(now); next.setHours(7, 0, 0, 0);
    const v = {
      phase: 'locked', reason: 'tasks', cycle: '2026-11-11', test: scene === 'test', strictOrder: !free,
      active: 'study', timerRunning: !paused, paused, tasks,
      openGroups: ['Музыка', 'Учёба в браузере', 'Редактор кода'], streak: 4,
      now: now.toISOString(), next: next.toISOString(),
    };
    if (scene === 'night') Object.assign(v, { phase: 'night', reason: 'night', tasks: [], active: '', openGroups: ['Музыка'] });
    if (scene === 'done') Object.assign(v, { phase: 'free', reason: 'done', tasks: [], active: '', streak: 5 });
    if (scene === 'update') Object.assign(v, { phase: 'free', reason: 'armed', tasks: [], active: '' });
    return v;
  }

  const journal = [
    { cycle: '2026-11-10', taskId: 'study', title: 'Учиться по roadmap', note: 'Разобрал буферизованные каналы и почему range по каналу ждёт close.', minutes: 62, at: '2026-11-10T08:31:00+03:00' },
    { cycle: '2026-11-09', taskId: 'study', title: 'Учиться по roadmap', note: 'Контексты: WithCancel, WithTimeout, передача по цепочке вызовов.', minutes: 60, at: '2026-11-09T08:44:00+03:00' },
    { cycle: '2026-11-08', taskId: 'study', title: 'Учиться по roadmap', note: 'sync.WaitGroup и errgroup. Написал воркер-пул.', minutes: 75, at: '2026-11-08T09:10:00+03:00' },
  ];

  const stats = {
    streak: 4, best: 9, doneDays: 23, avgRoutineMin: 71,
    days: Array.from({ length: 14 }, (_, i) => ({ cycle: '2026-11-' + String(i + 1).padStart(2, '0'), minutes: i === 6 ? 0 : 55 + ((i * 37) % 40), done: i !== 6 })),
    disables: [{ cycle: '2026-11-07', mode: 'tomorrow', reason: 'ранний выезд', at: '2026-11-07T07:12:00+03:00' }],
  };

  const handlers = {
    'boot': () => ({
      window: window.__dofirstWindow, lang, hotkey: 'Ctrl+Alt+D', version: '0.1.0',
      mode: scene === 'widget' || scene === 'warn' ? 'widget' : scene === 'update' ? 'dialog' : 'overlay',
      view: view(), firstLaunch: scene === 'first', tab: q.get('tab') || 'schedule',
      update: scene === 'update' ? { version: '0.2.0' } : null,
      theme: activeTheme(), themes: builtinThemes,
    }),
    'state': () => { if (!paused) remaining = Math.max(0, remaining - 1); return view(); },
    'task.pause': (p) => { paused = !!p.paused; return view(); },
    'task.complete': (p) => { if (remaining > 0 && p.id === 'study') throw { code: 'timer' }; if (!p.note) throw { code: 'note' }; return view(); },
    'task.activate': view,
    'task.launch': view,
    'lock.disable': view,
    'lock.test': view,
    'night.snooze': view,
    'stage.mode': () => null,
    'update.apply': () => new Promise(() => {}),
    'update.later': () => null,
    'update.check': () => ({ configured: false }),
    'settings.get': () => ({ settings }),
    'settings.save': (p) => {
      Object.assign(settings, p.settings);
      setTimeout(() => window.dofirst.emit('theme', activeTheme()), 60);
      return { settings };
    },
    'apps.list': () => {
      const here = ['zen.exe', 'code.exe', 'telegram.exe', 'spotify.exe', 'discord.exe', 'notion.exe', 'msedge.exe', 'studio64.exe'];
      const app = (id, name, category, exes) => ({ id, name, category, exes, installed: exes.some((e) => here.includes(e)) });
      return {
        categories: ['browsers', 'dev', 'ai', 'docs', 'study', 'music', 'chat', 'design', 'other'],
        apps: [
          app('chrome', 'Google Chrome', 'browsers', ['chrome.exe']), app('edge', 'Microsoft Edge', 'browsers', ['msedge.exe']),
          app('firefox', 'Firefox', 'browsers', ['firefox.exe']), app('zen', 'Zen Browser', 'browsers', ['zen.exe']),
          app('vscode', 'Visual Studio Code', 'dev', ['code.exe']), app('goland', 'GoLand', 'dev', ['goland64.exe']),
          app('androidstudio', 'Android Studio', 'dev', ['studio64.exe']),
          app('terminal', 'Терминал и PowerShell', 'dev', ['windowsterminal.exe', 'powershell.exe', 'pwsh.exe', 'cmd.exe']),
          app('claude', 'Claude', 'ai', ['claude.exe']), app('chatgpt', 'ChatGPT', 'ai', ['chatgpt.exe']), app('perplexity', 'Perplexity', 'ai', ['perplexity.exe']),
          app('obsidian', 'Obsidian', 'docs', ['obsidian.exe']), app('notion', 'Notion', 'docs', ['notion.exe']),
          app('spotify', 'Spotify', 'music', ['spotify.exe']), app('yandexmusic', 'Яндекс Музыка', 'music', ['яндекс музыка.exe']),
          app('telegram', 'Telegram', 'chat', ['telegram.exe']), app('discord', 'Discord', 'chat', ['discord.exe']),
          app('figma', 'Figma', 'design', ['figma.exe']),
        ],
        sites: [
          { id: 'claude', name: 'Claude', category: 'ai', titles: ['Claude'] }, { id: 'chatgpt', name: 'ChatGPT', category: 'ai', titles: ['ChatGPT'] },
          { id: 'deepseek', name: 'DeepSeek', category: 'ai', titles: ['DeepSeek'] }, { id: 'perplexity', name: 'Perplexity', category: 'ai', titles: ['Perplexity'] },
          { id: 'gemini', name: 'Gemini', category: 'ai', titles: ['Gemini'] }, { id: 'github', name: 'GitHub', category: 'dev', titles: ['GitHub'] },
          { id: 'godocs', name: 'Документация Go', category: 'dev', titles: ['Go Packages', 'The Go Programming Language', 'A Tour of Go'] },
          { id: 'roadmap', name: 'roadmap.sh', category: 'study', titles: ['roadmap.sh'] }, { id: 'pdf', name: 'PDF-файлы', category: 'docs', titles: ['.pdf'] },
        ],
        installed: [
          { name: 'Discord', exe: 'discord.exe', category: 'chat' }, { name: 'Notion', exe: 'notion.exe', category: 'docs' },
          { name: 'qBittorrent', exe: 'qbittorrent.exe', category: 'other' }, { name: 'Steam', exe: 'steam.exe', category: 'other' },
          { name: 'Visual Studio Code', exe: 'code.exe', category: 'dev' }, { name: 'Zen', exe: 'zen.exe', category: 'browsers' },
        ],
        open: [
          { name: 'Zen', exe: 'zen.exe', title: 'roadmap.sh — Zen Browser', category: 'browsers' },
          { name: 'Code', exe: 'code.exe', title: 'main.go — dofirst — Visual Studio Code', category: 'dev' },
          { name: 'Telegram', exe: 'telegram.exe', title: 'Telegram', category: 'chat' },
          { name: 'Spotify', exe: 'spotify.exe', title: 'Spotify Premium', category: 'music' },
        ],
      };
    },
    'journal.list': () => journal,
    'stats.get': () => stats,
    'panel.open': () => null,
    'window.drag': () => null,
    'window.minimize': () => null,
    'window.maximize': () => { setTimeout(() => window.dofirst.emit('window', { maximized: (handlers.maximized = !handlers.maximized) }), 50); return null; },
    'window.close': () => null,
    'menu.size': () => null,
    'menu.act': () => null,
  };

  window.chrome = window.chrome || {};
  window.chrome.webview = {
    postMessage(raw) {
      const msg = JSON.parse(raw);
      setTimeout(async () => {
        try {
          const fn = handlers[msg.method];
          if (!fn) throw { code: 'unknown', message: msg.method };
          window.dofirst.resolve(msg.id, { result: await fn(msg.params) });
        } catch (error) {
          window.dofirst.resolve(msg.id, { error });
        }
      }, 30);
    },
  };

  document.head.append(Object.assign(document.createElement('style'), { textContent: '*,*::before,*::after{animation:none!important}' }));

  window.addEventListener('load', () => setTimeout(() => {
    if (scene === 'warn') window.dofirst.emit('tabWarning', { seconds: 4 });
    if (scene === 'disable') window.dofirst.emit('askDisable');
    if (scene === 'toast') window.dofirst.emit('blocked', { app: 'Telegram', reason: 'app' });
  }, 200));
})();
