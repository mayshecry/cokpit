'use strict';

  const $  = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  

  const state = {
    token: localStorage.getItem('cockpit_token'),
    user: JSON.parse(localStorage.getItem('cockpit_user') || 'null'),
    orders: [],
    users: [],
    filter: 'All',
    search: '',
    sort: { key: 'id', dir: 'desc' },
    selectedId: null,
    detail: null,
    audit: [],
    comments: [],
    checklist: [],
    checklists: [],
    checklistSelId: null,
    picklistMeta: null,
    products: [],
    productSelId: null,
    productBlocks: [],
    orderManuals: [],
    manuals: [],
    manualsSelId: null,
    manualsMine: false,
    attention: null,
    notifications: [],
    unread: 0,
    loadingOrders: true,
    loadingUsers: true,
    lastSync: null,
    lastHomeSync: null,
    mine: false,
    sel: new Set(),
    rowLimit: 100,
    userBriefs: [],
    sse: null,
    sseOk: false,
    prevNotifIds: new Set(),

    
    config: {
      artikellocaties: {},
      klanten: {},
      uitgeslotenArtikelen: [],
      uitgeslotenKlanten: [],
      kleurregels: [],
      meta: {},
    },
    configBron: null,

    si: {
      customers: [],
      selectedCustomer: null,
      projects: [],
      selectedProject: null,
      sis: [],
      selectedSI: null,
      filter: 'All',
      loading: false,
    },
  };

  const rolePerms = {
    viewer:   ['orders:list', 'orders:view', 'audit:view', 'comments:read', 'notifications:read', 'si:list', 'si:view'],
    operator: ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'audit:view', 'comments:read', 'comments:post', 'notifications:read', 'scan:use', 'pick:use', 'si:list', 'si:view'],
    qc:       ['orders:list', 'orders:view', 'qc:submit', 'audit:view', 'comments:read', 'comments:post', 'notifications:read', 'si:list', 'si:view'],
    npi:      ['si:list', 'si:view', 'si:create', 'si:update', 'si:transition', 'si:projects', 'users:list'],
    admin:    ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'qc:submit', 'audit:view', 'users:manage', 'users:list', 'manuals:manage', 'comments:read', 'comments:post', 'scan:use', 'pick:use', 'config:manage', 'notifications:read', 'si:list', 'si:view', 'si:create', 'si:update', 'si:transition', 'si:projects', 'si:manage'],
  };

  const ROLES = ['viewer', 'operator', 'qc', 'npi', 'admin'];

  const SI_STATUSES = [
    { value: 'All', label: 'All' },
    { value: 'Concept', label: 'Concept' },
    { value: 'In Validation', label: 'In Validation' },
    { value: 'Accepted', label: 'Accepted' },
    { value: 'Outphased', label: 'Outphased' },
    { value: 'Blocked', label: 'Blocked' },
  ];

  const SI_ENVIRONMENTS = [
    { value: 'TEST', label: 'Test' },
    { value: 'ACCEPTATIE', label: 'Acceptatie' },
    { value: 'PRODUCTIE', label: 'Productie' },
  ];

  const SI_TRANSITIONS = {
    'Concept': ['In Validation', 'Blocked'],
    'In Validation': ['Accepted', 'Concept', 'Blocked'],
    'Accepted': ['Outphased', 'Blocked'],
    'Blocked': ['Concept', 'In Validation', 'Accepted'],
    'Outphased': [],
  };

  const STATUS_FILTERS = [
    { value: 'All',        label: 'All' },
    { value: 'Received',   label: 'Received' },
    { value: 'Processing', label: 'Processing' },
    { value: 'QC_Review',  label: 'QC review' },
    { value: 'Completed',  label: 'Completed' },
    { value: 'On_Hold',    label: 'On hold' },
  ];

  

  const can = (perm) => {
    if (!state.user) return false;
    if (state.user.role === 'admin') return true;
    const base = rolePerms[state.user.role] || [];
    return base.includes(perm) || (state.user.permissions || []).includes(perm);
  };

  const PERM_GROUPS = [
    { label: 'Orders', perms: [
      ['orders:list', 'View the order list'],
      ['orders:view', 'View order details, checklists and manuals'],
      ['orders:create', 'Create new orders'],
      ['orders:transition', 'Move orders to the next state'],
    ]},
    { label: 'Holds & QC', perms: [
      ['holds:create', 'Place holds on orders'],
      ['holds:resolve', 'Resolve active holds'],
      ['qc:submit', 'Submit QC checks'],
    ]},
    { label: 'Audit & comments', perms: [
      ['audit:view', 'View the audit trail'],
      ['comments:read', 'Read order comments'],
      ['comments:post', 'Post order comments'],
    ]},
    { label: 'Scanning & picking', perms: [
      ['scan:use', 'Use the barcode scanner'],
      ['pick:use', 'Tick off pick-list lines'],
    ]},
    { label: 'Users & departments', perms: [
      ['users:list', 'View users and departments'],
      ['users:manage', 'Manage users, departments and permission tables'],
    ]},
    { label: 'System integrations (SI)', perms: [
      ['si:list', 'View the SI list'],
      ['si:view', 'View SI details'],
      ['si:create', 'Create new SI'],
      ['si:update', 'Update existing SI'],
      ['si:transition', 'Change SI status'],
      ['si:projects', 'Link projects to SI'],
      ['si:manage', 'Full SI management (admin)'],
    ]},
    { label: 'Notifications & manuals', perms: [
      ['notifications:read', 'Read own notifications'],
      ['manuals:manage', 'Build product manuals and flag answers'],
    ]},
  ];

  

  const I18N = {
    en: {
      'nav.home': 'My Work', 'nav.orders': 'Orders', 'nav.pick': 'Pick lists',
      'nav.products': 'Products', 'nav.manuals': 'Manuals',
      'nav.users': 'Users', 'nav.config': 'Config',
      'login.signin': 'Sign in', 'login.signing': 'Signing in…',
      'stat.total': 'Total', 'stat.onTime': 'On time', 'stat.risk': 'At risk', 'stat.breached': 'Breached',
      'stat.pipeline': 'Pipeline', 'stat.service': 'Service level',
      'chip.all': 'All', 'chip.received': 'Received', 'chip.processing': 'Processing',
      'chip.qc': 'QC review', 'chip.completed': 'Completed', 'chip.hold': 'On hold',
      'th.id': 'ID', 'th.number': 'Order number', 'th.status': 'Status',
      'th.target': 'Target completes', 'th.sla': 'SLA', 'th.created': 'Created',
      'orders.total': '{n} orders', 'orders.total1': '1 order', 'orders.of': '{n} of {m} orders',
      'orders.empty.filtered': 'No matching orders', 'orders.empty.none': 'No orders yet',
      'orders.empty.filteredSub': 'Try a different status filter or search term.',
      'orders.empty.noneSub': 'Orders will appear here as soon as they are received.',
      'orders.subtitle.ok': 'Live view of the fulfilment pipeline. No SLA breaches.',
      'orders.subtitle.bad': '{n} order{s} past target — needs attention.',
      'btn.newOrder': 'New order', 'btn.refresh': 'Refresh', 'btn.export': 'Export CSV',
      'btn.transition': 'Transition', 'btn.hold': 'Hold', 'btn.submit': 'Submit',
      'btn.comment': 'Comment', 'btn.resolve': 'Resolve', 'btn.undo': 'Undo',
      'btn.tickOff': 'Tick off', 'btn.clear': 'Clear filters',
      'bulk.selected': '{n} selected', 'bulk.apply': 'Transition', 'bulk.resolve': 'Resolve holds',
      'loadMore': 'Load more',
      'detail.actions': 'Actions', 'detail.next': 'Move to next state',
      'detail.placeHold': 'Place a hold', 'detail.holdReason': 'Reason for hold',
      'detail.qc': 'Record QC check', 'detail.holds': 'Active holds',
      'detail.qcs': 'QC checks', 'detail.comments': 'Comments',
      'detail.audit': 'Audit trail', 'detail.scan': 'Scan', 'detail.assignee': 'Assignee',
      'detail.unassigned': 'Unassigned', 'detail.picklist': 'Pick checklist',
      'meta.target': 'Target completes', 'meta.created': 'Created', 'meta.updated': 'Last updated',
      'meta.holds': 'Holds', 'meta.none': 'None', 'meta.active': ' active',
      'toast.created': 'Order created', 'toast.moved': 'Status updated',
      'toast.holdPlaced': 'Hold placed', 'toast.holdResolved': 'Hold resolved',
      'toast.qc': 'QC check recorded', 'toast.commented': 'Comment added',
      'toast.ticked': 'Checklist item ticked off', 'toast.unticked': 'Checklist item reopened',
      'toast.undo': 'Undo', 'toast.retry': 'Retry',
      'users.add': 'Add user', 'users.backup': '⬇ Backup',
      'users.backupOk': 'Backup downloaded ({name})',
      'palette.pages': 'Pages', 'palette.actions': 'Actions',
      'palette.orders': 'Orders', 'palette.users': 'Users', 'palette.none': 'No matches',
      'act.newOrder': 'New order', 'act.export': 'Export current view (CSV)',
      'act.refresh': 'Refresh data', 'act.theme': 'Toggle dark mode',
      'act.lang': 'Switch language (EN/NL)', 'act.markRead': 'Mark notifications read',
      'act.backup': 'Download database backup', 'act.openOrder': 'Open order',
      'shortcut.myWork': 'My Work', 'shortcut.search': 'Focus search',
      'shortcut.new': 'New order', 'shortcut.export': 'Export CSV',
      'shortcut.palette': 'Command palette', 'shortcut.goto': 'Go to My Work',
      'shortcut.goOrders': 'Go to orders', 'shortcut.goUsers': 'Go to users',
      'shortcut.goConfig': 'Go to config (admin)', 'shortcut.refresh': 'Refresh data',
      'shortcut.close': 'Close panel / dialog', 'shortcut.cheats': 'This dialog',
      'dept.permissions': 'Department permissions',
      'dept.hint': 'Members inherit each granted permission on top of their role. Toggle the table, then save.',
      'dept.save': 'Save permissions',
      'dept.saved': 'Permissions saved',
      'dept.member': '{n} members',
      'dept.member1': '1 member',
      'dept.none': 'No extra permissions — members keep only their role.',
      'orders.customer': 'Customer',
      'orders.assignee': 'Assignee',
      'orders.quick': 'Move →',
      'orders.quickTitle': 'Move this order to the next state',
    },
  };

  I18N.nl = {
    'nav.home': 'Mijn werk', 'nav.orders': 'Orders', 'nav.pick': 'Picklijsten',
    'nav.products': 'Producten', 'nav.manuals': 'Handleidingen',
    'nav.users': 'Gebruikers', 'nav.config': 'Configuratie',
    'login.signin': 'Inloggen', 'login.signing': 'Inloggen…',
    'stat.total': 'Totaal', 'stat.onTime': 'Op tijd', 'stat.risk': 'Risico', 'stat.breached': 'Overschreden',
    'stat.pipeline': 'Pipeline', 'stat.service': 'Serviceniveau',
    'chip.all': 'Alles', 'chip.received': 'Ontvangen', 'chip.processing': 'In bewerking',
    'chip.qc': 'QC-controle', 'chip.completed': 'Afgerond', 'chip.hold': 'On hold',
    'th.number': 'Ordernummer', 'th.target': 'Doelgereed', 'th.created': 'Aangemaakt',
    'orders.total': '{n} orders', 'orders.total1': '1 order', 'orders.of': '{n} van {m} orders',
    'orders.empty.filtered': 'Geen matchende orders', 'orders.empty.none': 'Nog geen orders',
    'orders.empty.filteredSub': 'Probeer een andere statusfilter of zoekterm.',
    'orders.empty.noneSub': 'Orders verschijnen hier zodra ze binnenkomen.',
    'orders.subtitle.ok': 'Live overzicht van de pipeline. Geen SLA-overschrijdingen.',
    'orders.subtitle.bad': '{n} order{s} over doel — aandacht nodig.',
    'btn.newOrder': 'Nieuwe order', 'btn.refresh': 'Vernieuwen', 'btn.export': 'Export CSV',
    'btn.transition': 'Overzetten', 'btn.hold': 'Hold', 'btn.submit': 'Opslaan',
    'btn.comment': 'Reageren', 'btn.resolve': 'Opheffen', 'btn.undo': 'Ongedaan',
    'btn.tickOff': 'Afvinken', 'btn.clear': 'Filters wissen',
    'bulk.selected': '{n} geselecteerd', 'bulk.apply': 'Overzetten', 'bulk.resolve': 'Holds opheffen',
    'loadMore': 'Meer laden',
    'detail.actions': 'Acties', 'detail.next': 'Naar volgende status',
    'detail.placeHold': 'Hold plaatsen', 'detail.holdReason': 'Reden voor hold',
    'detail.qc': 'QC-controle vastleggen', 'detail.holds': 'Actieve holds',
    'detail.qcs': 'QC-controles', 'detail.comments': 'Opmerkingen',
    'detail.audit': 'Audit trail', 'detail.scan': 'Scan', 'detail.assignee': 'Toegewezen aan',
    'detail.unassigned': 'Niet toegewezen', 'detail.picklist': 'Pickchecklist',
    'meta.target': 'Doelgereed', 'meta.created': 'Aangemaakt', 'meta.updated': 'Laatst gewijzigd',
    'meta.holds': 'Holds', 'meta.none': 'Geen', 'meta.active': ' actief',
    'toast.created': 'Order aangemaakt', 'toast.moved': 'Status bijgewerkt',
    'toast.holdPlaced': 'Hold geplaatst', 'toast.holdResolved': 'Hold opgeheven',
    'toast.qc': 'QC-controle vastgelegd', 'toast.commented': 'Opmerking toegevoegd',
    'toast.ticked': 'Regel afgevinkt', 'toast.unticked': 'Regel heropend',
    'toast.undo': 'Ongedaan', 'toast.retry': 'Opnieuw',
    'users.add': 'Gebruiker toevoegen', 'users.backup': '⬇ Backup',
    'users.backupOk': 'Backup gedownload ({name})',
    'palette.pages': "Pagina's", 'palette.actions': 'Acties',
    'palette.orders': 'Orders', 'palette.users': 'Gebruikers', 'palette.none': 'Geen treffers',
    'act.newOrder': 'Nieuwe order', 'act.export': 'Huidige weergave exporteren (CSV)',
    'act.refresh': 'Gegevens verversen', 'act.theme': 'Donkere modus wisselen',
    'act.lang': 'Taal wisselen (NL/EN)', 'act.markRead': 'Meldingen gelezen maken',
    'act.backup': 'Databasebackup downloaden', 'act.openOrder': 'Order openen',
    'shortcut.myWork': 'Mijn werk', 'shortcut.search': 'Zoeken',
    'shortcut.new': 'Nieuwe order', 'shortcut.export': 'Export CSV',
    'shortcut.palette': 'Commandopalet', 'shortcut.goto': 'Ga naar Mijn werk',
    'shortcut.goOrders': 'Ga naar orders', 'shortcut.goUsers': 'Ga naar gebruikers',
    'shortcut.goConfig': 'Ga naar configuratie', 'shortcut.refresh': 'Gegevens verversen',
    'shortcut.close': 'Paneel / dialoog sluiten', 'shortcut.cheats': 'Dit venster',
    'dept.permissions': 'Afdeling permissies',
    'dept.hint': 'Leden erven elke toegekende permissie bovenop hun rol. Vink aan, sla daarna op.',
    'dept.save': 'Permissies opslaan',
    'dept.saved': 'Permissies opgeslagen',
    'dept.member': '{n} leden',
    'dept.member1': '1 lid',
    'dept.none': 'Geen extra permissies — leden houden alleen hun rol.',
    'orders.customer': 'Klant',
    'orders.assignee': 'Toegewezen aan',
    'orders.quick': 'Overzetten →',
    'orders.quickTitle': 'Zet deze order naar de volgende status',
  };

  const lang = () => localStorage.getItem('cockpit_lang')
    || ((navigator.language || 'en').toLowerCase().startsWith('nl') ? 'nl' : 'en');
  const t = (key, vars) => {
    let s = (I18N[lang()] || I18N.en)[key] ?? I18N.en[key] ?? key;
    if (vars) for (const [k, v] of Object.entries(vars)) s = s.split('{' + k + '}').join(v);
    return s;
  };

  

  const BASE_TITLE = 'Cockpit — Order Operations';

  function applyPrefs() {
    const theme = localStorage.getItem('cockpit_theme') || 'system';
    const dark = theme === 'dark'
      || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    if (dark) document.documentElement.setAttribute('data-theme', 'dark');
    else document.documentElement.removeAttribute('data-theme');
    document.documentElement.setAttribute('data-density', localStorage.getItem('cockpit_density') || 'cozy');
    const metaScheme = document.querySelector('meta[name="color-scheme"]');
    if (metaScheme) metaScheme.setAttribute('content', dark ? 'dark' : 'light');
  }
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    if ((localStorage.getItem('cockpit_theme') || 'system') === 'system') applyPrefs();
  });

  function drawFaviconBadge(n) {
    const link = $('#favicon');
    if (!link) return;
    const c = document.createElement('canvas');
    c.width = c.height = 64;
    const g = c.getContext('2d');
    g.fillStyle = '#ff8200';
    g.beginPath();
    g.roundRect(0, 0, 64, 64, 14);
    g.fill();
    if (n > 0) {
      g.fillStyle = '#c0392f';
      g.beginPath();
      g.arc(46, 18, 17, 0, Math.PI * 2);
      g.fill();
      g.fillStyle = '#fff';
      g.font = '800 20px system-ui, sans-serif';
      g.textAlign = 'center';
      g.textBaseline = 'middle';
      g.fillText(n > 9 ? '9+' : String(n), 46, 19);
    } else {
      g.strokeStyle = '#fff';
      g.lineWidth = 5;
      g.beginPath();
      g.moveTo(20, 34);
      g.lineTo(29, 43);
      g.lineTo(45, 24);
      g.stroke();
    }
    link.href = c.toDataURL('image/png');
  }

  function updateBadges() {
    const n = state.unread || 0;
    document.title = (n > 0 ? `(${n}) ` : '') + BASE_TITLE;
    drawFaviconBadge(n);
  }

  function beep() {
    try {
      const ctx = beep.ctx || (beep.ctx = new (window.AudioContext || window.webkitAudioContext)());
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.connect(gain); gain.connect(ctx.destination);
      osc.type = 'sine';
      osc.frequency.setValueAtTime(880, ctx.currentTime);
      osc.frequency.setValueAtTime(1174, ctx.currentTime + 0.09);
      gain.gain.setValueAtTime(0.0001, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.12, ctx.currentTime + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.25);
      osc.start(); osc.stop(ctx.currentTime + 0.28);
    } catch {  }
  }

  

  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]
  ));

  const fmtTime = (iso) => {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    return d.toLocaleString(undefined, {
      day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit',
    });
  };

  
  const relTime = (iso) => {
    if (!iso) return '';
    const t = new Date(iso).getTime();
    if (Number.isNaN(t)) return '';
    let diff = t - Date.now();
    const future = diff >= 0;
    diff = Math.abs(diff);
    const m = Math.round(diff / 60000);
    let out;
    if (m < 1) out = 'just now';
    else if (m < 60) out = m + 'm';
    else if (m < 1440) out = Math.floor(m / 60) + 'h ' + (m % 60) + 'm';
    else out = Math.floor(m / 1440) + 'd ' + Math.floor((m % 1440) / 60) + 'h';
    if (out === 'just now') return out;
    return future ? 'in ' + out : out + ' ago';
  };

  const initials = (u) => String(u.displayName || u.username || '?')
    .trim().split(/\s+/).slice(0, 2).map((p) => p[0]).join('').toUpperCase();

  const isHeld = (o) => typeof o.status === 'string' && o.status.startsWith('Held_');

  const statusLabel = (s) => (typeof s === 'string' && s.startsWith('Held_'))
    ? 'On Hold · ' + s.slice(5).replace(/_/g, ' ')
    : String(s).replace(/_/g, ' ');

  const statusClass = (s) => (typeof s === 'string' && s.startsWith('Held_')) ? 'On_Hold' : s;

  const slaOf = (o) => {
    
    if (o.sla && o.sla.status) return o.sla.status;
    const rem = new Date(o.targetCompletionAt).getTime() - Date.now();
    if (Number.isNaN(rem)) return 'ON_TIME';
    if (rem < 0) return 'BREACHED';
    if (rem <= 4 * 3600 * 1000) return 'WARNING';
    return 'ON_TIME';
  };

  const nextStates = (st) => {
    if (st.startsWith('Held_') || st === 'Completed') return [];
    switch (st) {
      case 'Received':   return ['Processing', 'QC_Review'];
      case 'Processing': return ['Received', 'QC_Review'];
      case 'QC_Review':  return ['Processing', 'Completed'];
      default: return [];
    }
  };

  const badge = (cls, text) => `<span class="badge ${cls}"><span class="dot"></span>${esc(text)}</span>`;

  const ICON_OK  = '<svg class="t-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>';
  const ICON_ERR = '<svg class="t-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><path d="M12 8v5M12 16.5v.01"/></svg>';

  function toast(msg, isErr, action) {
    const stack = $('#toast-stack');
    const el = document.createElement('div');
    el.className = 'toast' + (isErr ? ' err' : '');
    el.innerHTML = `${isErr ? ICON_ERR : ICON_OK}<span class="t-msg">${esc(msg)}</span>
      ${action ? `<button class="t-act" type="button">${esc(action.label)}</button>` : ''}
      <button class="t-close" type="button" aria-label="Dismiss">&times;</button>`;
    const kill = () => {
      el.classList.add('leaving');
      setTimeout(() => el.remove(), 200);
    };
    el.querySelector('.t-close').addEventListener('click', kill);
    if (action) {
      el.querySelector('.t-act').addEventListener('click', () => { kill(); action.fn(); });
    }
    stack.appendChild(el);
    setTimeout(kill, isErr ? 8000 : 3500);
    while (stack.children.length > 4) stack.firstElementChild.remove();
  }

  

  let modalCleanup = null;

  
  function modal({ title, description, fields = [], confirmLabel = 'Confirm', cancelLabel = 'Cancel', danger = false, wide = false, bodyHtml = '', hideConfirm = false }) {
    return new Promise((resolve) => {
      const root = $('#modal-root');

      const fieldHtml = fields.map((f) => {
        const id = 'mf-' + f.name;
        let control;
        if (f.type === 'select') {
          control = `<select id="${id}" name="${f.name}">${
            (f.options || []).map((o) => {
              const val = typeof o === 'string' ? o : o.value;
              const lbl = typeof o === 'string' ? o : o.label;
              return `<option value="${esc(val)}"${val === f.value ? ' selected' : ''}>${esc(lbl)}</option>`;
            }).join('')
          }</select>`;
        } else if (f.type === 'checkbox') {
          control = `<label class="checkbox-label"><input id="${id}" name="${f.name}" type="checkbox"${f.checked ? ' checked' : ''}> ${esc(f.label)}</label>`;
        } else if (f.type === 'textarea') {
          control = `<textarea id="${id}" name="${f.name}" rows="3" placeholder="${esc(f.placeholder || '')}">${esc(f.value || '')}</textarea>`;
        } else {
          control = `<input id="${id}" name="${f.name}" type="${f.type || 'text'}" value="${esc(f.value || '')}"
            placeholder="${esc(f.placeholder || '')}"${f.required ? ' required' : ''}
            ${f.type === 'password' ? 'autocomplete="new-password"' : 'autocomplete="off"'} spellcheck="false">`;
        }
        return `<div class="field ${f.half ? 'half' : ''}">
            <label for="${id}">${esc(f.label)}</label>${control}
            ${f.hint ? `<span class="field-hint">${esc(f.hint)}</span>` : ''}
          </div>`;
      }).join('');

      root.innerHTML = `
        <div class="modal${wide ? ' wide' : ''}" role="dialog" aria-modal="true" aria-labelledby="modal-title">
          <form id="modal-form">
            <div class="modal-head">
              ${danger ? `<div class="modal-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z"/><path d="M12 9v4M12 17h.01"/></svg></div>` : ''}
              <h2 id="modal-title">${esc(title)}</h2>
              ${description ? `<p>${esc(description)}</p>` : ''}
            </div>
            <div class="modal-body">${bodyHtml}${fieldHtml}<p class="form-error hidden" id="modal-error"></p></div>
            <div class="modal-foot">
              <button type="button" class="btn btn-secondary" data-modal-cancel>${esc(cancelLabel)}</button>
              ${hideConfirm ? '' : `<button type="submit" class="btn ${danger ? 'btn-danger' : 'btn-primary'}">${esc(confirmLabel)}</button>`}
            </div>
          </form>
        </div>`;
      root.classList.remove('hidden');

      const form = $('#modal-form', root);
      const close = (result) => {
        document.removeEventListener('keydown', onKey, true);
        var modalBody = $('.modal-body', root);
        if (modalBody && modalBody._checklistHandler) {
          modalBody.removeEventListener('click', modalBody._checklistHandler);
          delete modalBody._checklistHandler;
        }
        root.classList.add('hidden');
        root.innerHTML = '';
        modalCleanup = null;
        resolve(result);
      };
      modalCleanup = () => close(null);

      const onKey = (e) => {
        if (e.key === 'Escape') { e.stopPropagation(); close(null); }
      };
      document.addEventListener('keydown', onKey, true);

      root.addEventListener('mousedown', (e) => { if (e.target === root) close(null); });
      $('[data-modal-cancel]', root).addEventListener('click', () => close(null));

      form.addEventListener('submit', (e) => {
        e.preventDefault();
        const data = {};
        for (const f of fields) {
          const el = form.elements[f.name];
          data[f.name] = el
            ? (el.type === 'checkbox' ? (el.checked ? 'on' : '') : el.value)
            : '';
          if (f.required && !String(data[f.name]).trim()) {
            const err = $('#modal-error', root);
            err.textContent = f.label + ' is required.';
            err.classList.remove('hidden');
            if (el) el.focus();
            return;
          }
        }
        close(data);
      });

      const focusTarget = fields.find((f) => f.autofocus) || fields[0];
      const el = focusTarget && form.elements[focusTarget.name];
      if (el) el.focus(); else $('[data-modal-cancel]', root).focus();
    });
  }

  const confirmDialog = (title, description, confirmLabel) =>
    modal({ title, description, confirmLabel: confirmLabel || 'Confirm', danger: true })
      .then((r) => r !== null);

  async function api(method, path, body) {
    let res;
    try {
      res = await fetch(path, {
        method,
        headers: {
          'Content-Type': 'application/json',
          ...(state.token ? { Authorization: 'Bearer ' + state.token } : {}),
        },
        body: body ? JSON.stringify(body) : undefined,
      });
    } catch {
      
      throw new Error('Cannot reach the API. Check that the server is running.');
    }
    const text = await res.text();
    let data = null;
    if (text) { try { data = JSON.parse(text); } catch {  } }
    if (res.status === 401 && !path.endsWith('/auth/login')) {
      logout(false);
      const err = new Error('Session expired, please sign in again');
      err.status = 401;
      throw err;
    }
    if (!res.ok) {
      const err = new Error((data && data.error && data.error.message) || ('HTTP ' + res.status));
      err.status = res.status;
      err.data = data; 
      throw err;
    }
    return data;
  }

