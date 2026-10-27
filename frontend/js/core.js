'use strict';

  const $  = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
  

  const state = window.cockpitState = {
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
    lastScan: null,
    ordersRaw: null,
    attentionRaw: null,
    lastSseAt: 0,

    
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

  // Fallback copy of the role -> permission table. It MUST stay identical to
  // pkg/auth/auth.go; /api/v1/auth/permissions refreshes it at runtime so the
  // UI can never drift away from what the server actually enforces.
  const rolePerms = {
    viewer:   ['orders:list', 'orders:view', 'audit:view', 'comments:read', 'notifications:read', 'si:list', 'si:view', 'process:view'],
    operator: ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'audit:view', 'comments:read', 'comments:post', 'notifications:read', 'scan:use', 'pick:use', 'si:list', 'si:view', 'process:view', 'process:advance', 'process:escalate'],
    qc:       ['orders:list', 'orders:view', 'orders:transition', 'qc:submit', 'audit:view', 'comments:read', 'comments:post', 'notifications:read', 'manuals:manage', 'si:list', 'si:view', 'process:view', 'process:escalate'],
    npi:      ['si:list', 'si:view', 'si:create', 'si:update', 'si:transition', 'si:projects', 'users:list', 'process:view', 'process:manage', 'integrations:manage'],
    sc:       ['orders:list', 'orders:view', 'audit:view', 'comments:read', 'comments:post', 'notifications:read', 'manuals:manage', 'manuals:approve', 'customers:manage', 'si:list', 'si:view', 'users:list', 'process:view'],
    admin:    ['orders:list', 'orders:create', 'orders:view', 'orders:transition', 'holds:create', 'holds:resolve', 'qc:submit', 'audit:view', 'users:manage', 'users:list', 'manuals:manage', 'manuals:approve', 'customers:manage', 'comments:read', 'comments:post', 'scan:use', 'pick:use', 'notifications:read', 'si:list', 'si:view', 'si:create', 'si:update', 'si:transition', 'si:projects', 'si:manage', 'process:view', 'process:advance', 'process:escalate', 'process:manage', 'integrations:manage'],
  };

  // Replace the local table with the authoritative one from the server.
  function applyPermissionMatrix(d) {
    if (!d || !d.roles) return;
    for (const role of Object.keys(d.roles)) {
      if (Array.isArray(d.roles[role])) rolePerms[role] = d.roles[role].slice();
    }
  }

  let permissionMatrixRequest = null;
  async function loadPermissionMatrix() {
    if (!permissionMatrixRequest) {
      permissionMatrixRequest = api('GET', '/api/v1/auth/permissions')
        .then((d) => { applyPermissionMatrix(d); return d; })
        .catch(() => null);
    }
    return permissionMatrixRequest;
  }

  // Legacy UI-only permission names mapped onto the real server permission.
  const PERM_ALIASES = { 'config:manage': 'users:manage' };

  const ROLES = ['viewer', 'operator', 'qc', 'npi', 'sc', 'admin'];

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
    perm = PERM_ALIASES[perm] || perm;
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
      ['manuals:approve', 'Approve a manual for production use'],
      ['customers:manage', 'Manage customers and assign SC coordinators'],
    ]},
    { label: 'Order process (SWI)', perms: [
      ['process:view', 'View the process board, stages and task lists'],
      ['process:advance', 'Advance stages and tick process tasks'],
      ['process:escalate', 'Raise and resolve process escalations'],
      ['process:manage', 'Force stage changes and run auto escalation'],
      ['integrations:manage', 'Claim, complete and retry external sync jobs'],
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
      'shortcut.goWork': 'Workload board',
      'shortcut.goOrders': 'Go to orders', 'shortcut.goUsers': 'Go to users',
      'shortcut.goConfig': 'Go to config (admin)', 'shortcut.refresh': 'Refresh data',
      'shortcut.close': 'Close panel / dialog', 'shortcut.cheats': 'This dialog',
      'shortcut.rowNav': 'Move between order rows', 'shortcut.rowOpen': 'Open focused order',
      'shortcut.rowSelect': 'Select / deselect focused row',
      'dept.permissions': 'Department permissions',
      'dept.hint': 'Members inherit each granted permission on top of their role. Toggle the table, then save.',
      'dept.save': 'Save permissions',
      'dept.saved': 'Permissions saved',
      'dept.member': '{n} members',
      'dept.member1': '1 member',
      'dept.none': 'No extra permissions — members keep only their role.',
      'orders.customer': 'Customer',
      'orders.assignee': 'Assignee',
      'info.debit': 'Debit number', 'info.device': 'Device', 'info.asset': 'Asset number',
      'info.config': 'Configuration', 'info.omni': 'Omnitracker ticket',
      'work.start': 'Start Work', 'work.resume': 'Resume', 'work.inprogress': 'In progress',
      'work.busy': 'Another user is clocked in on this order', 'work.title': 'Work session',
      'work.readySub': 'Clock in to start the guided flow. Every step has manual checks — Next unlocks once everything is ticked off.',
      'work.step.intake': 'Intake & verify', 'work.step.intaked': 'Confirm the order and the device in front of you match.',
      'work.step.build': 'Pick & configure', 'work.step.buildd': 'Pick the parts and apply the configuration.',
      'work.step.test': 'Self-test & hand to QC', 'work.step.testd': 'Test the device, then send it to QC review.',
      'work.step.wrap': 'Wrap up & complete', 'work.step.wrapd': 'Final checks, then complete the order.',
      'work.check.c_data': 'Order data complete (customer, debit, ticket)',
      'work.check.c_device': 'Device present and undamaged',
      'work.check.c_asset': 'Asset tag matches the order',
      'work.check.c_config': 'Configuration applied as specified',
      'work.check.c_firmware': 'Firmware and software up to date',
      'work.check.c_test': 'Device fully tested and working',
      'work.check.c_notes': 'Work notes added for QC',
      'work.check.c_clean': 'Device cleaned and packed',
      'work.check.c_report': 'Result registered in Omnitracker',
      'work.picklist': 'Pick list', 'work.pickempty': 'No pick list linked to this order — continue with the checks below.',
      'work.checksLeft': 'check(s) left', 'work.pickLeft': 'pick(s) left',
      'work.qcwait': 'Waiting for QC pass — a QC reviewer or admin must pass this order before it can be completed.',
      'work.qcok': 'QC passed — you can complete this order.',
      'work.next': 'Next step', 'work.back': 'Back', 'work.complete': 'Complete order',
      'work.short.intake': 'Intake', 'work.short.build': 'Pick & config', 'work.short.test': 'Self-test & QC', 'work.short.wrap': 'Wrap up',
      'work.clockout': 'Clock out', 'work.clockedout': 'Clocked out', 'work.elapsed': 'Time on this order',
      'work.target': 'Target', 'work.doneTitle': 'Order completed', 'work.doneSub': 'All steps ticked off. Session logged.',
      'work.backToOrders': 'Back to orders', 'work.viewOrder': 'View order', 'work.assignedYou': 'assigned to you',
      'work.manuals': 'Manuals', 'work.manualLeft': 'manual block(s) left', 'work.exitNow': 'Exit for now',
      'work.exitToast': 'Session stays open — resume anytime from Start Work.',
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
    'shortcut.goWork': 'Werklastbord',
      'shortcut.goOrders': 'Ga naar orders', 'shortcut.goUsers': 'Ga naar gebruikers',
    'shortcut.goConfig': 'Ga naar configuratie', 'shortcut.refresh': 'Gegevens verversen',
    'shortcut.close': 'Paneel / dialoog sluiten', 'shortcut.cheats': 'Dit venster',
    'shortcut.rowNav': 'Tussen orderregels bewegen', 'shortcut.rowOpen': 'Gerichte order openen',
    'shortcut.rowSelect': 'Gerichte regel (de)selecteren',
    'dept.permissions': 'Afdeling permissies',
    'dept.hint': 'Leden erven elke toegekende permissie bovenop hun rol. Vink aan, sla daarna op.',
    'dept.save': 'Permissies opslaan',
    'dept.saved': 'Permissies opgeslagen',
    'dept.member': '{n} leden',
    'dept.member1': '1 lid',
    'dept.none': 'Geen extra permissies — leden houden alleen hun rol.',
    'orders.customer': 'Klant',
    'orders.assignee': 'Toegewezen aan',
    'info.debit': 'Debitnummer', 'info.device': 'Apparaat', 'info.asset': 'Assetnummer',
    'info.config': 'Configuratie', 'info.omni': 'Omnitracker-ticket',
    'work.start': 'Start werk', 'work.resume': 'Hervat', 'work.inprogress': 'Bezig',
    'work.busy': 'Een andere gebruiker is ingeklokt op deze order', 'work.title': 'Werksessie',
    'work.readySub': 'Klok in om de begeleide flow te starten. Elke stap heeft handmatige checks — Volgende gaat open als alles is afgevinkt.',
    'work.step.intake': 'Intake & controle', 'work.step.intaked': 'Bevestig dat order en apparaat voor je neus overeenkomen.',
    'work.step.build': 'Picken & configureren', 'work.step.buildd': 'Pick de onderdelen en voer de configuratie toe.',
    'work.step.test': 'Zelftest & overdracht aan QC', 'work.step.testd': 'Test het apparaat en stuur het naar QC-review.',
    'work.step.wrap': 'Afronden & voltooien', 'work.step.wrapd': 'Laatste checks, daarna de order voltooien.',
    'work.check.c_data': 'Orderdata compleet (klant, debit, ticket)',
    'work.check.c_device': 'Apparaat aanwezig en onbeschadigd',
    'work.check.c_asset': 'Asset-tag komt overeen met de order',
    'work.check.c_config': 'Configuratie volgens specificatie toegepast',
    'work.check.c_firmware': 'Firmware en software up-to-date',
    'work.check.c_test': 'Apparaat volledig getest en werkend',
    'work.check.c_notes': 'Werknotities toegevoegd voor QC',
    'work.check.c_clean': 'Apparaat schoongemaakt en verpakt',
    'work.check.c_report': 'Resultaat geregistreerd in Omnitracker',
    'work.picklist': 'Picklijst', 'work.pickempty': 'Geen picklijst gekoppeld aan deze order — ga verder met de checks hieronder.',
    'work.checksLeft': 'check(s) over', 'work.pickLeft': 'pick(s) over',
    'work.qcwait': 'Wachten op QC-akkoord — een QC-reviewer of admin moet deze order eerst goedkeuren.',
    'work.qcok': 'QC akkoord — je kunt de order voltooien.',
    'work.next': 'Volgende stap', 'work.back': 'Terug', 'work.complete': 'Order voltooien',
    'work.short.intake': 'Intake', 'work.short.build': 'Pick & config', 'work.short.test': 'Zelftest & QC', 'work.short.wrap': 'Afronden',
    'work.clockout': 'Uitklokken', 'work.clockedout': 'Uitgeklokt', 'work.elapsed': 'Tijd aan deze order',
    'work.target': 'Target', 'work.doneTitle': 'Order afgerond', 'work.doneSub': 'Alle stappen afgevinkt. Sessie geregistreerd.',
    'work.backToOrders': 'Terug naar orders', 'work.viewOrder': 'Bekijk order', 'work.assignedYou': 'aan jou toegewezen',
    'work.manuals': 'Manuals', 'work.manualLeft': 'manualblok(ken) over', 'work.exitNow': 'Even sluiten',
    'work.exitToast': 'Sessie blijft open — hervat wanneer je wilt via Start werk.',
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
    const theme = localStorage.getItem('cockpit_theme') || 'dark';
    const dark = theme !== 'light'
      && (theme === 'dark' || theme !== 'system' || window.matchMedia('(prefers-color-scheme: dark)').matches);
    if (dark) document.documentElement.setAttribute('data-theme', 'dark');
    else document.documentElement.removeAttribute('data-theme');
    document.documentElement.setAttribute('data-density', localStorage.getItem('cockpit_density') || 'spacious');
    const metaScheme = document.querySelector('meta[name="color-scheme"]');
    if (metaScheme) metaScheme.setAttribute('content', dark ? 'dark' : 'light');
  }
  // Older Safari only has addListener(); a missing matchMedia must not abort
  // this file, because everything else lives in the same script scope.
  const darkSchemeQuery = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  if (darkSchemeQuery) {
    const onSchemeChange = () => {
      if ((localStorage.getItem('cockpit_theme') || 'dark') === 'system') applyPrefs();
    };
    if (darkSchemeQuery.addEventListener) darkSchemeQuery.addEventListener('change', onSchemeChange);
    else if (darkSchemeQuery.addListener) darkSchemeQuery.addListener(onSchemeChange);
  }

  function drawFaviconBadge(n) {
    const link = $('#favicon');
    if (!link) return;
    const c = document.createElement('canvas');
    c.width = c.height = 64;
    const g = c.getContext && c.getContext('2d');
    // A null context (canvas disabled, memory pressure, ancient browser) or a
    // missing roundRect() must never break the app shell.
    if (!g) return;
    g.fillStyle = '#ff8200';
    g.beginPath();
    if (typeof g.roundRect === 'function') g.roundRect(0, 0, 64, 64, 14);
    else if (typeof g.arcTo === 'function') {
      const r = 14;
      g.moveTo(r, 0);
      g.arcTo(64, 0, 64, 64, r);
      g.arcTo(64, 64, 0, 64, r);
      g.arcTo(0, 64, 0, 0, r);
      g.arcTo(0, 0, 64, 0, r);
      g.closePath();
    } else {
      g.rect(0, 0, 64, 64);
    }
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
    try { link.href = c.toDataURL('image/png'); } catch { /* tainted canvas */ }
  }

  function updateBadges() {
    const n = state.unread || 0;
    if (n === updateBadges.last) return;                                   
    updateBadges.last = n;
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

                                                                              
                                                                           
  const fmtCell = (iso) => {
    if (!iso) return '—';
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return '—';
    const opts = { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit', hour12: false };
    if (d.getFullYear() !== new Date().getFullYear()) opts.year = 'numeric';
    return d.toLocaleString(undefined, opts);
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

                                                                                      
  const SLA_STORAGE_KEY = 'cockpit_customer_slas';
  const SLA_DEFAULTS_KEY = 'cockpit_sla_defaults';
  const BUILTIN_CUSTOMER_SLAS = {
    'kenter': { name: 'Kenter', workdays: 2, excludeWeekends: true },
    'dela':   { name: 'Dela',   workdays: 5, excludeWeekends: true },
  };

  const SLA = {
    // Mirrors order.ValidateTargetCompletion on the server. A target in the
    // past is legitimate (an order registered after it arrived, or imported
    // from Omnitracker) — it simply means the order is already overdue. Only
    // implausible values are rejected, so a typo cannot create an order that
    // looks years overdue.
    TARGET_FLOOR: new Date('2000-01-01T00:00:00Z').getTime(),
    TARGET_HORIZON_DAYS: 3650,

    targetProblem(date, nowMs) {
      // Duck-typed on purpose: `instanceof Date` is false across realms.
      const ms = date && typeof date.getTime === 'function' ? date.getTime() : NaN;
      if (!Number.isFinite(ms)) return 'Enter a valid target date and time.';
      const now = nowMs === undefined ? Date.now() : nowMs;
      if (ms < SLA.TARGET_FLOOR) return 'Target date looks wrong — pick a date after 2000.';
      if (ms > now + SLA.TARGET_HORIZON_DAYS * 86400000) return 'Target date is more than 10 years out — check the year.';
      return '';
    },

    // Parses a datetime-local value. Returns {iso, past, problem}; an empty
    // value yields {iso: null} meaning "use the server default".
    parseTargetInput(value) {
      const raw = String(value == null ? '' : value).trim();
      if (!raw) return { iso: null, past: false, problem: '' };
      const dt = new Date(raw);
      const problem = SLA.targetProblem(dt);
      if (problem) return { iso: null, past: false, problem };
      return { iso: dt.toISOString(), past: dt.getTime() < Date.now(), problem: '' };
    },

    getDefaults() {
      let saved = {};
      try { saved = JSON.parse(localStorage.getItem(SLA_DEFAULTS_KEY) || '{}') || {}; } catch {}
      return {
        defaultWorkdays: Number(saved.defaultWorkdays) > 0 ? Number(saved.defaultWorkdays) : 2,
        excludeWeekends: saved.excludeWeekends !== false,
        endOfDayHour: Number.isFinite(Number(saved.endOfDayHour)) ? Number(saved.endOfDayHour) : 17,
      };
    },

    setDefaults(patch) {
      const next = { ...SLA.getDefaults(), ...(patch || {}) };
      try { localStorage.setItem(SLA_DEFAULTS_KEY, JSON.stringify(next)); } catch {}
      return next;
    },

    excludeWeekends() {
      return SLA.getDefaults().excludeWeekends !== false;
    },

    _readMap() {
      let raw = {};
      try { raw = JSON.parse(localStorage.getItem(SLA_STORAGE_KEY) || '{}') || {}; } catch {}
      const merged = {};
      Object.keys(BUILTIN_CUSTOMER_SLAS).forEach((k) => {
        merged[k] = { ...BUILTIN_CUSTOMER_SLAS[k] };
      });
      if (state && state.config && state.config.customerSlas && typeof state.config.customerSlas === 'object') {
        Object.entries(state.config.customerSlas).forEach(([k, v]) => {
          const key = String(k || '').trim().toLowerCase();
          if (!key) return;
          const wd = typeof v === 'number' ? v : Number(v && v.workdays);
          if (wd > 0) {
            merged[key] = {
              name: (v && v.name) || (merged[key] && merged[key].name) || String(k).trim(),
              workdays: wd,
              excludeWeekends: v && v.excludeWeekends !== undefined ? !!v.excludeWeekends : true,
            };
          }
        });
      }
      Object.entries(raw).forEach(([k, v]) => {
        const key = String(k || '').trim().toLowerCase();
        if (!key) return;
        if (v && v.deleted && !BUILTIN_CUSTOMER_SLAS[key]) {
          delete merged[key];
          return;
        }
        const wd = typeof v === 'number' ? v : Number(v && v.workdays);
        if (wd > 0) {
          merged[key] = {
            name: (v && v.name) || (merged[key] && merged[key].name) || String(k).trim(),
            workdays: wd,
            excludeWeekends: v && v.excludeWeekends !== undefined ? !!v.excludeWeekends : true,
          };
        }
      });
      return merged;
    },

    _writeMap(map) {
      try { localStorage.setItem(SLA_STORAGE_KEY, JSON.stringify(map)); } catch {}
      if (state && state.config) {
        state.config.customerSlas = map;
        try { localStorage.setItem('cockpit_config', JSON.stringify(state.config)); } catch {}
      }
    },

    allCustomerRules() {
      const m = SLA._readMap();
      return Object.values(m).sort((a, b) => String(a.name).localeCompare(String(b.name)));
    },

    getCustomerRule(customerName) {
      const key = String(customerName || '').trim().toLowerCase();
      if (!key) return null;
      const m = SLA._readMap();
      return m[key] || null;
    },

    getCustomerDays(customerName) {
      const r = SLA.getCustomerRule(customerName);
      return r ? Number(r.workdays) : null;
    },

    effectiveDays(customerName) {
      const d = SLA.getCustomerDays(customerName);
      return d && d > 0 ? d : SLA.getDefaults().defaultWorkdays;
    },

    setCustomerDays(customerName, workdays, excludeWeekends = true) {
      const cleanName = String(customerName || '').trim();
      const key = cleanName.toLowerCase();
      if (!key) return null;
      const wd = Math.max(1, Math.min(60, Math.round(Number(workdays) || SLA.getDefaults().defaultWorkdays)));
      const m = SLA._readMap();
      m[key] = {
        name: (m[key] && m[key].name) || cleanName,
        workdays: wd,
        excludeWeekends: excludeWeekends !== false,
      };
      SLA._writeMap(m);
      return m[key];
    },

    removeCustomerDays(customerName) {
      const key = String(customerName || '').trim().toLowerCase();
      if (!key) return;
      let raw = {};
      try { raw = JSON.parse(localStorage.getItem(SLA_STORAGE_KEY) || '{}') || {}; } catch {}
      if (BUILTIN_CUSTOMER_SLAS[key]) {
        raw[key] = { ...BUILTIN_CUSTOMER_SLAS[key] };
      } else {
        delete raw[key];
      }
      SLA._writeMap(raw);
    },

    isWeekend(d) {
      const dt = d instanceof Date ? d : new Date(d);
      const day = dt.getDay();
      return day === 0 || day === 6;
    },

    addWorkdays(fromDate, workdays, opts = {}) {
      const defs = SLA.getDefaults();
      const skipWeekends = opts.excludeWeekends !== undefined ? !!opts.excludeWeekends : defs.excludeWeekends;
      const days = Math.max(0, Number(workdays) || 0);
      const cur = new Date(fromDate ? new Date(fromDate).getTime() : Date.now());
      let skippedWeekendDays = 0;

                                                                                   
      if (skipWeekends) {
        while (SLA.isWeekend(cur)) {
          cur.setDate(cur.getDate() + 1);
          skippedWeekendDays++;
        }
      }

      let remaining = Math.round(days);
      while (remaining > 0) {
        cur.setDate(cur.getDate() + 1);
        if (skipWeekends && SLA.isWeekend(cur)) {
          skippedWeekendDays++;
          continue;
        }
        remaining--;
      }

      if (!opts.preserveTime) {
        const hr = Number.isFinite(Number(opts.endOfDayHour)) ? Number(opts.endOfDayHour) : defs.endOfDayHour;
        cur.setHours(hr, 0, 0, 0);
      }

      return {
        date: cur,
        workdays: Math.round(days),
        skippedWeekendDays,
        excludeWeekends: skipWeekends,
      };
    },

    targetForCustomer(customerName, fromDate, opts = {}) {
      const rule = SLA.getCustomerRule(customerName);
      const defs = SLA.getDefaults();
      const workdays = rule ? rule.workdays : defs.defaultWorkdays;
      const excludeWeekends = rule ? rule.excludeWeekends !== false : defs.excludeWeekends;
      const res = SLA.addWorkdays(fromDate || new Date(), workdays, {
        ...opts,
        excludeWeekends,
      });
      return {
        ...res,
        customerName: String(customerName || '').trim(),
        hasCustomRule: !!rule,
        rule,
      };
    },

                                                                                   
    workingMsBetween(startMs, endMs, excludeWeekends) {
      const skip = excludeWeekends !== undefined ? !!excludeWeekends : SLA.excludeWeekends();
      const t0 = Number(startMs);
      const t1 = Number(endMs);
      if (!Number.isFinite(t0) || !Number.isFinite(t1)) return NaN;
      if (!skip) return t1 - t0;
      if (t0 === t1) return 0;

      const sign = t1 >= t0 ? 1 : -1;
      let a = Math.min(t0, t1);
      const b = Math.max(t0, t1);
      let total = 0;

      while (a < b) {
        const d = new Date(a);
        const nextMidnight = new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1, 0, 0, 0, 0).getTime();
        const segEnd = Math.min(b, nextMidnight);
        const day = d.getDay();
        if (day !== 0 && day !== 6) {
          total += (segEnd - a);
        }
        a = segEnd;
      }
      return sign * total;
    },

    countBetween(startDate, endDate) {
      const a = new Date(startDate ? new Date(startDate).getTime() : Date.now());
      const b = new Date(endDate ? new Date(endDate).getTime() : Date.now());
      a.setHours(0, 0, 0, 0);
      b.setHours(0, 0, 0, 0);
      const forward = b >= a;
      const cur = new Date(forward ? a : b);
      const target = new Date(forward ? b : a);
      let workdays = 0;
      let weekendDays = 0;
      while (cur < target) {
        cur.setDate(cur.getDate() + 1);
        if (SLA.isWeekend(cur)) weekendDays++;
        else workdays++;
      }
      return { workdays: forward ? workdays : -workdays, weekendDays };
    },

    mergeCustomersWithDefaults(customers) {
      const list = Array.isArray(customers) ? customers.slice() : [];
      const seen = new Set(list.map((c) => String(c.name || '').trim().toLowerCase()));
      const rules = SLA.allCustomerRules();
      const defaultNumbers = { kenter: '1010', dela: '1020' };
      rules.forEach((r) => {
        const k = String(r.name || '').trim().toLowerCase();
        if (k && !seen.has(k)) {
          seen.add(k);
          list.push({
            name: r.name,
            number: defaultNumbers[k] || '',
            managed: true,
            custom: false,
            hasFlow: false,
            slaDays: r.workdays,
          });
        }
      });
      return list.map((c) => ({
        ...c,
        slaDays: SLA.getCustomerDays(c.name),
      })).sort((a, b) => String(a.name || '').localeCompare(String(b.name || '')));
    },

    _seeded: false,
    async ensureDefaultCustomers() {
      if (SLA._seeded || !state || !state.user) return;
      SLA._seeded = true;
      try {
        const d = await api('GET', '/api/v1/customers');
        const existing = new Set((d.customers || []).map((c) => String(c.name || '').trim().toLowerCase()));
        const toSeed = [
          { name: 'Kenter', number: '1010' },
          { name: 'Dela', number: '1020' },
        ];
        for (const item of toSeed) {
          if (!existing.has(item.name.toLowerCase())) {
            try { await api('POST', '/api/v1/customers', item); } catch {}
          }
        }
      } catch {}
    },
  };
  window.SLA = SLA;

  const slaOf = (o) => {
    if (!o) return 'ON_TIME';
    if (o.status === 'Completed') return 'ON_TIME';
    const targetMs = o.targetCompletionAt ? new Date(o.targetCompletionAt).getTime() : NaN;
    if (!Number.isNaN(targetMs)) {
      const rule = o.customerName ? SLA.getCustomerRule(o.customerName) : null;
      const skipWeekends = rule ? (rule.excludeWeekends !== false) : SLA.excludeWeekends();
      const rem = SLA.workingMsBetween(Date.now(), targetMs, skipWeekends);
      if (!Number.isNaN(rem)) {
        if (rem < 0) return 'BREACHED';
        if (rem <= 4 * 3600 * 1000) return 'WARNING';
        return 'ON_TIME';
      }
    }
    if (o.sla && o.sla.status) return o.sla.status;
    return 'ON_TIME';
  };

                                                                              
                                                                         
                                                                                
  const nextStates = (st) => {
    if (st.startsWith('Held_') || st === 'Completed') return [];
    switch (st) {
      case 'Received':   return ['Processing', 'QC_Review'];
      case 'Processing': return ['QC_Review', 'Received'];
      case 'QC_Review':  return ['Completed', 'Processing', 'Received'];
      default: return [];
    }
  };

                                                                          
  async function copyText(v) {
    try { await navigator.clipboard.writeText(v); return true; } catch {                    }
    try {
      const ta = document.createElement('textarea');
      ta.value = v; ta.style.position = 'fixed'; ta.style.opacity = '0';
      document.body.appendChild(ta); ta.select();
      const ok = document.execCommand('copy');
      ta.remove();
      return ok;
    } catch { return false; }
  }
  document.addEventListener('click', (e) => {
    const el = e.target.closest('[data-copy]');
    if (!el) return;
    e.preventDefault(); e.stopPropagation();
    copyText(el.dataset.copy).then((ok) => toast(ok ? 'Copied ' + el.dataset.copy : 'Copy failed', !ok));
  });

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

  
  // ---------------------------------------------------------------------
  // Chip picker: a searchable multi-select used for people, teams, customers
  // and products. Values are kept in the DOM as `.chip-pick-chosen` elements so
  // any custom section (e.g. per-customer routing) can read them the same way.
  // ---------------------------------------------------------------------
  const ChipsPicker = {
    // html renders the picker. `options` is [{value, label, hint, group}].
    html({ id, options = [], value = [], max = 0, placeholder = 'Search…', emptyHint = '' } = {}) {
      const selected = (value || []).map((v) => (typeof v === 'string' ? v : v.value)).filter(Boolean);
      const norm = new Map((options || []).map((o) => [String(o.value).toLowerCase(), o]));
      const chips = selected.map((v) => {
        const opt = norm.get(String(v).toLowerCase());
        const label = opt ? (opt.label || opt.value) : v;
        return `<span class="chip-pick-chip" data-value="${esc(v)}">${esc(label)}<button type="button" class="chip-pick-x" data-chip-remove="${esc(v)}" aria-label="Remove ${esc(label)}">&times;</button></span>`;
      }).join('');
      const list = (options || []).map((o) => {
        const group = o.group ? ` data-group="${esc(o.group)}"` : '';
        const hint = o.hint ? ` data-hint="${esc(o.hint)}"` : '';
        return `<button type="button" class="chip-pick-option" data-pick-value="${esc(o.value)}"${group}${hint}>
          <span class="chip-pick-option-label">${esc(o.label || o.value)}</span>
          ${o.hint ? `<span class="chip-pick-option-hint">${esc(o.hint)}</span>` : ''}
        </button>`;
      }).join('');
      return `<div class="chip-pick" id="${esc(id)}" data-max="${max || 0}">
        <div class="chip-pick-box" data-chip-box>
          <span class="chip-pick-chosen" data-chip-chosen>${chips}</span>
          <input type="text" class="chip-pick-search" data-chip-search placeholder="${esc(placeholder)}" autocomplete="off" spellcheck="false">
        </div>
        <div class="chip-pick-menu hidden" data-chip-menu>${list || '<div class="chip-pick-empty">Nothing to pick from yet.</div>'}</div>
        <div class="chip-pick-foot"><span class="chip-pick-count" data-chip-count></span><span class="chip-pick-note" data-chip-note>${esc(emptyHint)}</span></div>
      </div>`;
    },

    // bind wires one picker rendered by html(). onChange receives the values.
    bind(root, { max = 0, onChange } = {}) {
      const wrap = typeof root === 'string' ? $(root) : root;
      if (!wrap || wrap.dataset.chipBound === '1') return wrap;
      wrap.dataset.chipBound = '1';
      const box = $('[data-chip-box]', wrap);
      const chosen = $('[data-chip-chosen]', wrap);
      const search = $('[data-chip-search]', wrap);
      const menu = $('[data-chip-menu]', wrap);
      // The limit is read live so a form can change it (e.g. ASSIGN allows one
      // recipient while NOTIFY allows many) without re-rendering the picker.
      const limit = () => Number(wrap.dataset.max || max || 0);
      const count = $('[data-chip-count]', wrap);

      const values = () => $$('.chip-pick-chip', chosen).map((el) => el.dataset.value);
      const refresh = () => {
        const current = values();
        $$('.chip-pick-option', menu).forEach((btn) => {
          const picked = current.some((v) => v.toLowerCase() === btn.dataset.pickValue.toLowerCase());
          btn.classList.toggle('is-picked', picked);
          btn.disabled = false;
        });
        if (count) {
          const max2 = limit();
          count.textContent = current.length === 0
            ? 'None selected'
            : `${current.length} selected${max2 > 0 && current.length >= max2 ? ' (max reached)' : ''}`;
        }
        wrap.dataset.values = JSON.stringify(current);
        if (typeof onChange === 'function') onChange(current);
      };
      const add = (value, label) => {
        const current = values();
        if (current.some((v) => v.toLowerCase() === String(value).toLowerCase())) return;
        // _canAdd lets a form enforce a budget shared by several pickers
        // (e.g. ASSIGN allows one recipient across people + teams).
        if (typeof wrap._canAdd === 'function' && !wrap._canAdd(value, current)) return;
        const max2 = limit();
        if (max2 > 0 && current.length >= max2) return;
        const span = document.createElement('span');
        span.className = 'chip-pick-chip';
        span.dataset.value = value;
        span.innerHTML = `${esc(label || value)}<button type="button" class="chip-pick-x" data-chip-remove="${esc(value)}" aria-label="Remove ${esc(label || value)}">&times;</button>`;
        chosen.appendChild(span);
        refresh();
      };
      const remove = (value) => {
        $$('.chip-pick-chip', chosen).forEach((el) => {
          if (el.dataset.value.toLowerCase() === String(value).toLowerCase()) el.remove();
        });
        refresh();
      };

      const filter = () => {
        const term = (search.value || '').trim().toLowerCase();
        let shown = 0;
        $$('.chip-pick-option', menu).forEach((btn) => {
          const haystack = (btn.dataset.pickValue + ' ' + btn.textContent).toLowerCase();
          const match = !term || haystack.includes(term);
          btn.classList.toggle('hidden', !match);
          if (match) shown += 1;
        });
        menu.classList.remove('hidden');
        const emptyMsg = $('.chip-pick-filtered-empty', menu);
        if (emptyMsg) emptyMsg.remove();
        if (shown === 0) {
          const note = document.createElement('div');
          note.className = 'chip-pick-empty chip-pick-filtered-empty';
          note.textContent = 'No match for “' + (search.value || '') + '”.';
          menu.appendChild(note);
        }
      };

      box.addEventListener('click', (e) => {
        if (e.target.closest('[data-chip-remove]')) {
          e.preventDefault();
          remove(e.target.closest('[data-chip-remove]').dataset.chipRemove);
          return;
        }
        if (e.target === box || e.target === chosen) {
          search.focus();
          filter();
        }
      });
      search.addEventListener('focus', filter);
      search.addEventListener('input', filter);
      search.addEventListener('keydown', (e) => {
        if (e.key === 'Backspace' && !search.value) {
          const chips = values();
          if (chips.length) remove(chips[chips.length - 1]);
        }
        if (e.key === 'Enter') {
          e.preventDefault();
          const first = $$('.chip-pick-option:not(.hidden)', menu)[0];
          if (first) {
            add(first.dataset.pickValue, $('.chip-pick-option-label', first).textContent.trim());
            search.value = '';
            filter();
          }
        }
        if (e.key === 'Escape') menu.classList.add('hidden');
      });
      menu.addEventListener('click', (e) => {
        const btn = e.target.closest('[data-pick-value]');
        if (!btn) return;
        e.preventDefault();
        add(btn.dataset.pickValue, $('.chip-pick-option-label', btn).textContent.trim());
        search.value = '';
        filter();
        search.focus();
      });
      const onDocClick = (e) => {
        if (!wrap.contains(e.target)) menu.classList.add('hidden');
      };
      document.addEventListener('mousedown', onDocClick);

      wrap._chipValues = values;
      wrap._chipAdd = add;
      wrap._chipRemove = remove;
      wrap._chipRefresh = refresh;
      refresh();
      return wrap;
    },

    // values reads back whatever is currently chosen.
    values(selectorOrEl) {
      const wrap = typeof selectorOrEl === 'string' ? $(selectorOrEl) : selectorOrEl;
      if (!wrap) return [];
      if (typeof wrap._chipValues === 'function') return wrap._chipValues();
      return $$('.chip-pick-chip', wrap).map((el) => el.dataset.value);
    },

    set(selectorOrEl, values, options) {
      const wrap = typeof selectorOrEl === 'string' ? $(selectorOrEl) : selectorOrEl;
      if (!wrap) return;
      const chosen = $('[data-chip-chosen]', wrap);
      if (!chosen) return;
      chosen.innerHTML = '';
      const norm = new Map(((options || []).map((o) => [String(o.value).toLowerCase(), o])));
      (values || []).forEach((v) => {
        const opt = norm.get(String(v).toLowerCase());
        const span = document.createElement('span');
        span.className = 'chip-pick-chip';
        span.dataset.value = v;
        span.innerHTML = `${esc((opt && (opt.label || opt.value)) || v)}<button type="button" class="chip-pick-x" data-chip-remove="${esc(v)}" aria-label="Remove">&times;</button>`;
        chosen.appendChild(span);
      });
      if (typeof wrap._chipRefresh === 'function') wrap._chipRefresh();
    },
  };
  window.ChipsPicker = ChipsPicker;

  function modal({ title, description, fields = [], confirmLabel = 'Confirm', cancelLabel = 'Cancel', danger = false, wide = false, bodyHtml = '', afterHtml = '', hideConfirm = false, onMount, collect, validate }) {
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
        } else if (f.type === 'chips') {
          control = ChipsPicker.html({
            id: id,
            options: f.options || [],
            value: Array.isArray(f.value) ? f.value : (f.value ? [f.value] : []),
            max: f.max || 0,
            placeholder: f.placeholder || 'Search…',
            emptyHint: f.emptyHint || '',
          });
        } else if (f.type === 'combobox') {
          const listId = `${id}-list`;
          control = `<input id="${id}" name="${f.name}" type="text" value="${esc(f.value || '')}"
            placeholder="${esc(f.placeholder || '')}"${f.required ? ' required' : ''}
            list="${listId}" autocomplete="off" spellcheck="false">
          <datalist id="${listId}">${
            (f.options || []).map((o) => {
              const val = typeof o === 'string' ? o : o.value;
              return `<option value="${esc(val)}"></option>`;
            }).join('')
          }</datalist>`;
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
            <div class="modal-body">${bodyHtml}${fieldHtml}${afterHtml}<p class="form-error hidden" id="modal-error"></p></div>
            <div class="modal-foot">
              <button type="button" class="btn btn-secondary" data-modal-cancel>${esc(cancelLabel)}</button>
              ${hideConfirm ? '' : `<button type="submit" class="btn ${danger ? 'btn-danger' : 'btn-primary'}">${esc(confirmLabel)}</button>`}
            </div>
          </form>
        </div>`;
      root.classList.remove('hidden');

      const form = $('#modal-form', root);
      fields.filter((f) => f.type === 'chips').forEach((f) => {
        ChipsPicker.bind($('#mf-' + f.name, root), {
          max: f.max || 0,
          onChange: (values) => { if (typeof f.onChange === 'function') f.onChange(values, root); },
        });
      });
      if (typeof onMount === 'function') onMount(form, root);
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
        const showError = (msg, focusEl) => {
          const err = $('#modal-error', root);
          err.textContent = msg;
          err.classList.remove('hidden');
          if (focusEl) focusEl.focus();
        };
        for (const f of fields) {
          const el = form.elements[f.name];
          if (f.type === 'chips') {
            data[f.name] = ChipsPicker.values($('#mf-' + f.name, root));
          } else {
            data[f.name] = el
              ? (el.type === 'checkbox' ? (el.checked ? 'on' : '') : el.value)
              : '';
            if (f.type === 'checkbox') data[f.name] = el && el.checked ? 'on' : '';
          }
          const empty = Array.isArray(data[f.name]) ? data[f.name].length === 0 : !String(data[f.name]).trim();
          if (f.required && empty) {
            showError(f.label + ' is required.', el);
            return;
          }
          if (f.max && Array.isArray(data[f.name]) && f.max > 0 && data[f.name].length > f.max) {
            showError(`${f.label}: pick at most ${f.max}.`, el);
            return;
          }
        }
        if (typeof collect === 'function') {
          const extra = collect(data, form, root);
          if (extra) Object.assign(data, extra);
        }
        if (typeof validate === 'function') {
          const problem = validate(data, form, root);
          if (problem) {
            showError(problem);
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
        credentials: 'same-origin',
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


                                                                           
window.countUp = function (el, to, opts) {
  const o = opts || {};
  const dec = o.decimals || 0;
  const suf = o.suffix || '';
  const dur = o.duration || 620;
  const fmt = (v) => v.toFixed(dec) + suf;
  if (window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches) {
    el.textContent = fmt(to);
    return;
  }
  const from = o.from || 0;
  const t0 = performance.now();
  const step = (t) => {
    const p = Math.min(1, (t - t0) / dur);
    const e = 1 - Math.pow(1 - p, 3);
    el.textContent = fmt(from + (to - from) * e);
    if (p < 1) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
};

window.hydrateCounts = function (root) {
  (root || document).querySelectorAll('[data-count]').forEach((el, i) => {
    const to = parseFloat(el.dataset.count);
    const dec = parseInt(el.dataset.dec || '0', 10);
    const suf = el.dataset.suffix || '';
    const cur = parseFloat((el.textContent || '').replace(/[^0-9.-]/g, ''));
    const from = Number.isFinite(cur) ? cur : 0;
    if (from !== to) el.textContent = from.toFixed(dec) + suf;
    setTimeout(() => window.countUp(el, to, { decimals: dec, suffix: suf, from }), 50 + i * 35);
  });
};
