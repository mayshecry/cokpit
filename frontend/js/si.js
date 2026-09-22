"use strict";


var siEventsBound = false;
var customerListBound = false;

function escapeSI(str) {
  return String(str || '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function siStatusBadge(status) {
  var cls = String(status).toLowerCase().replace(/[\s/]/g, '-');
  return '<span class="status-badge status-si-' + cls + '">' + escapeSI(status) + '</span>';
}

function siEnvBadge(env) {
  var cls = String(env).toLowerCase();
  return '<span class="env-badge env-' + cls + '">' + escapeSI(env) + '</span>';
}

async function loadSIView() {
  state.si.loading = true;
  await loadSICustomers();
  await Promise.all([loadSIProjects(), loadSISIs()]);
  state.si.loading = false;
  renderSICustomerList();
  renderSITable();
  renderSIFilters();
  bindSIEvents();
}

async function refreshSIData(options) {
  options = options || {};
  var promises = [];

  if (options.reloadCustomers) {
    await loadSICustomers();
  }
  if (options.reloadProjects) {
    promises.push(loadSIProjects());
  }
  if (options.reloadSIs !== false) {
    promises.push(loadSISIs());
  }

  await Promise.all(promises);

  if (options.reloadCustomers) {
    renderSICustomerList();
  }
  if (options.reloadProjects) {
    renderSIFilters();
  }
  renderSITable();
}

async function loadSICustomers() {
  try {
    var data = await api('GET', '/api/v1/si/customers');
    state.si.customers = data.customers || [];
  } catch (err) {
    toast('Failed to load customers: ' + err.message, 'error');
    state.si.customers = [];
    state.si.selectedCustomer = null;
    state.si.selectedProject = null;
    state.si.projects = [];
    return;
  }
  if (state.si.selectedCustomer) {
    var fresh = null;
    for (var i = 0; i < state.si.customers.length; i++) {
      if (state.si.customers[i].id === state.si.selectedCustomer.id) { fresh = state.si.customers[i]; break; }
    }
    if (fresh) {
      state.si.selectedCustomer = fresh;
    } else {
      state.si.selectedCustomer = null;
      state.si.selectedProject = null;
      state.si.projects = [];
    }
  }
}

async function loadSIProjects() {
  var sel = state.si.selectedCustomer;
  var valid = false;
  if (sel) {
    for (var i = 0; i < state.si.customers.length; i++) {
      if (state.si.customers[i].id === sel.id) { valid = true; break; }
    }
  }
  if (!valid) {
    state.si.selectedCustomer = null;
    state.si.selectedProject = null;
    state.si.projects = [];
    return;
  }
  try {
    var data = await api('GET', '/api/v1/si/customers/' + sel.number + '/projects');
    state.si.projects = data.projects || [];
  } catch (err) {
    toast('Failed to load projects: ' + err.message, 'error');
    state.si.projects = [];
  }
}

async function loadSISIs() {
  var params = new URLSearchParams();
  if (state.si.filter && state.si.filter !== 'All') {
    params.set('status', state.si.filter);
  }
  if (state.si.selectedProject) {
    params.set('project', state.si.selectedProject.id);
  } else if (state.si.selectedCustomer) {
    params.set('customer', state.si.selectedCustomer.number);
  }
  try {
    var data = await api('GET', '/api/v1/si/sis?' + params.toString());
    state.si.sis = data.sis || [];
  } catch (err) {
    toast('Failed to load SIs: ' + err.message, 'error');
    state.si.sis = [];
  }
}

function renderSICustomerList() {
  var root = $('#si-customer-list');
  if (!root) return;
  if (state.si.customers.length === 0) {
    root.innerHTML = '<p class="muted si-empty">No customers yet.</p>';
    return;
  }
  var canAssign = can('customers:manage');
  var scs = (state.userBriefs || []).filter(function(u) { return u.role === 'sc'; });
  root.innerHTML = state.si.customers.map(function(c) {
    var active = state.si.selectedCustomer && state.si.selectedCustomer.id === c.id ? ' active' : '';
    var coord = canAssign
      ? '<span class="si-coord" data-stop><select data-coord="' + c.id + '" aria-label="Service coordinator">' +
          '<option value="">— no SC —</option>' +
          scs.map(function(u) {
            return '<option value="' + escapeSI(u.username) + '"' + (c.assignedTo === u.username ? ' selected' : '') + '>' +
              escapeSI(u.displayName || u.username) + '</option>';
          }).join('') +
        '</select></span>'
      : (c.assignedTo ? '<span class="si-coord muted">SC: @' + escapeSI(c.assignedTo) + '</span>' : '');
    return '<div class="si-customer-item' + active + '" data-id="' + c.id + '" data-number="' + escapeSI(c.number) + '">' +
      '<strong>' + escapeSI(c.number) + '</strong>' +
      '<span class="muted">' + escapeSI(c.name) + '</span>' +
      coord +
      '</div>';
  }).join('');
  if (canAssign && !root._coordBound) {
    root._coordBound = true;
    root.addEventListener('click', function(e) {
      if (e.target.closest('[data-stop]')) e.stopPropagation();
    });
    root.addEventListener('change', async function(e) {
      var sel = e.target.closest('[data-coord]');
      if (!sel) return;
      try {
        await api('POST', '/api/v1/si/customers/' + sel.dataset.coord + '/coordinator', { username: sel.value });
        toast(sel.value ? 'Customer assigned to @' + sel.value : 'Coordinator cleared');
        await loadSICustomers();
      } catch (err) { toast(err.message, true); }
    });
  }
}

function renderSIFilters() {
  var statusSel = $('#si-status-filter');
  var projectSel = $('#si-project-filter');
  if (!statusSel || !projectSel) return;
  statusSel.innerHTML = SI_STATUSES.map(function(s) {
    return '<option value="' + s.value + '"' + (state.si.filter === s.value ? ' selected' : '') + '>' + s.label + '</option>';
  }).join('');
  var projectOptions = state.si.projects.map(function(p) {
    return '<option value="' + p.id + '"' + (state.si.selectedProject && state.si.selectedProject.id === p.id ? ' selected' : '') + '>' + escapeSI(p.code) + ' - ' + escapeSI(p.name) + '</option>';
  }).join('');
  projectSel.innerHTML = '<option value="">All projects</option>' + projectOptions;
  var group = projectSel.closest ? projectSel.closest('.si-filter-group') : null;
  if (group) {
    if (state.si.selectedCustomer) group.classList.remove('hidden');
    else group.classList.add('hidden');
  }
}

function renderSITable() {
  var tbody = $('#si-table tbody');
  var countEl = $('#si-count');
  if (!tbody) return;
  var sis = state.si.sis;
  if (countEl) {
    countEl.textContent = sis.length === 0 ? 'No SIs' : (sis.length + ' SI' + (sis.length === 1 ? '' : 's'));
  }
  if (sis.length === 0) {
    tbody.innerHTML = '<tr><td colspan="9" class="muted">No System Integrations found.</td></tr>';
    return;
  }
  tbody.innerHTML = sis.map(function(si) {
    var projectId = si.primaryProjectId || (si.projectIds && si.projectIds[0]) || '-';
    var project = null;
    for (var i = 0; i < state.si.projects.length; i++) {
      if (state.si.projects[i].id === projectId) { project = state.si.projects[i]; break; }
    }
    var projectName = project ? project.code : (projectId === '-' ? '-' : '#' + projectId);
    var projectClickable = project ? 'si-project-link' : '';
    return '<tr class="si-row-clickable" data-id="' + si.id + '">' +
      '<td class="num">' + si.id + '</td>' +
      '<td>' + escapeSI(si.code) + '</td>' +
      '<td>' + escapeSI(si.name) + '</td>' +
      '<td>' + siStatusBadge(si.statusLabel || si.status) + '</td>' +
      '<td>' + siEnvBadge(si.environment) + '</td>' +
      '<td class="' + projectClickable + '" data-project-id="' + (project ? project.id : '') + '">' + escapeSI(projectName) + '</td>' +
      '<td class="num">v' + si.version + '</td>' +
      '<td>' + (si.updatedAt ? fmtTime(si.updatedAt) : '-') + '</td>' +
      '<td class="right"><button type="button" class="btn btn-sm btn-ghost si-view-btn" data-id="' + si.id + '">View</button></td>' +
      '</tr>';
  }).join('');
}

async function viewSI(id) {
  try {
    var data = await api('GET', '/api/v1/si/sis/' + id);
    state.si.selectedSI = data.si;
    renderSIDetail();
  } catch (err) {
    toast('Failed to load SI: ' + err.message, 'error');
  }
}

function renderSIDetail() {
  var root = $('#si-detail-body');
  var detailEl = $('#si-detail');
  if (!root || !detailEl || !state.si.selectedSI) return;
  var si = state.si.selectedSI;
  var allowedTransitions = SI_TRANSITIONS[si.status] || [];
  var canTransition = can('si:transition') && allowedTransitions.length > 0;
  var canEdit = can('si:update');
  var canManageProjects = can('si:projects');
  var transitionsHtml = '';
  if (canTransition) {
    transitionsHtml = '<div class="si-transitions"><h4>Lifecycle Transitions</h4><div class="si-transition-btns">';
    for (var i = 0; i < allowedTransitions.length; i++) {
      var label = allowedTransitions[i];
      var btnLabel = '-> ' + label;
      if (label === 'Blocked') btnLabel = '🔒 Block';
      else if (si.status === 'Blocked') btnLabel = '🔓 Release: ' + label;
      transitionsHtml += '<button type="button" class="btn btn-sm btn-secondary si-transition-btn" data-status="' + label + '">' + btnLabel + '</button>';
    }
    transitionsHtml += '</div></div>';
  }
  var html = '<div class="si-detail-head">' +
    '<div><h3>' + escapeSI(si.code) + ' <span class="muted">' + (si.versionLabel || ('v' + si.version)) + '</span></h3>' +
    '<p class="si-detail-name">' + escapeSI(si.name) + '</p></div>' +
    '<button type="button" class="icon-btn si-close-detail" aria-label="Close detail">' +
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M18 6 6 18M6 6l12 12"/></svg>' +
    '</button></div>' +
    '<div class="si-detail-meta">' +
    '<div class="si-meta-row"><span class="si-meta-label">Status</span><span class="si-meta-value">' + siStatusBadge(si.statusLabel || si.status) + '</span></div>' +
    '<div class="si-meta-row"><span class="si-meta-label">Environment</span><span class="si-meta-value">' + siEnvBadge(si.environment) + '</span></div>' +
    '<div class="si-meta-row"><span class="si-meta-label">Primary Project</span><span class="si-meta-value">#' + si.primaryProjectId + '</span></div>' +
    '<div class="si-meta-row"><span class="si-meta-label">Linked Projects</span><span class="si-meta-value">' + (si.projectIds || []).join(', ') + '</span></div>' +
    '<div class="si-meta-row"><span class="si-meta-label">Created by</span><span class="si-meta-value">' + escapeSI(si.createdBy || '-') + '</span></div>' +
    '<div class="si-meta-row"><span class="si-meta-label">Updated</span><span class="si-meta-value">' + (si.updatedAt ? fmtTime(si.updatedAt) : '-') + '</span></div>' +
    '</div>';
  if (si.description) {
    html += '<div class="si-detail-desc"><h4>Description</h4><p>' + escapeSI(si.description) + '</p></div>';
  }
  if (si.config) {
    html += '<div class="si-detail-config"><h4>NPI Configuration (Automatic)</h4>';
    var cfg = si.config;
    if (cfg.workInstructions) html += '<div class="si-meta-row"><span class="si-meta-label">Work Instructions</span><span class="si-meta-value">' + escapeSI(cfg.workInstructions) + '</span></div>';
    if (cfg.swi) html += '<div class="si-meta-row"><span class="si-meta-label">SWI</span><span class="si-meta-value">' + escapeSI(cfg.swi) + '</span></div>';
    if (cfg.workflow) html += '<div class="si-meta-row"><span class="si-meta-label">Workflow</span><span class="si-meta-value si-pre">' + escapeSI(cfg.workflow) + '</span></div>';
    if (cfg.qcProfile) html += '<div class="si-meta-row"><span class="si-meta-label">QC Profile</span><span class="si-meta-value">' + escapeSI(cfg.qcProfile) + '</span></div>';
    if (cfg.automations) html += '<div class="si-meta-row"><span class="si-meta-label">Automations</span><span class="si-meta-value">' + escapeSI(cfg.automations) + '</span></div>';
    if (cfg.escalationFlow && cfg.escalationFlow.length > 0) html += '<div class="si-meta-row"><span class="si-meta-label">Escalation Flow</span><span class="si-meta-value">' + cfg.escalationFlow.map(function(d) { return '<span class="dept-tag">' + escapeSI(d) + '</span>'; }).join(' ') + '</span></div>';
    html += '</div>';
  }
  html += transitionsHtml;
  html += '<div class="si-detail-actions">';
  if (canEdit) html += '<button type="button" class="btn btn-secondary" id="si-edit-btn">Edit</button>';
  if (canManageProjects) html += '<button type="button" class="btn btn-secondary" id="si-projects-btn">Manage Projects</button>';
  html += '<button type="button" class="btn btn-secondary si-checklist-btn" id="si-checklist-btn">📋 Checklist</button>';
  html += '</div>';
  root.innerHTML = html;
  detailEl.classList.remove('hidden');
  bindSIDetailEvents();
}

var siDetailEventsBound = false;

function bindSIDetailEvents() {
  if (siDetailEventsBound) return;
  siDetailEventsBound = true;

  var detailBody = $('#si-detail-body');
  if (!detailBody) return;

  detailBody.addEventListener('click', async function(e) {
    var target = e.target;

    if (target.closest('.si-close-detail')) {
      closeSIDetail();
      return;
    }

    if (target.id === 'si-edit-btn' || target.closest('#si-edit-btn')) {
      if (state.si.selectedSI) openSIEditModal(state.si.selectedSI);
      return;
    }

    if (target.id === 'si-projects-btn' || target.closest('#si-projects-btn')) {
      if (state.si.selectedSI) openSIManageProjectsModal(state.si.selectedSI);
      return;
    }

    if (target.id === 'si-checklist-btn' || target.closest('#si-checklist-btn')) {
      if (state.si.selectedSI) {
        var projectId = state.si.selectedSI.primaryProjectId;
        if (projectId) openProjectChecklistModal(projectId);
        else toast('No primary project linked.', 'error');
      }
      return;
    }

    var transitionBtn = target.closest('.si-transition-btn');
    if (transitionBtn && state.si.selectedSI) {
      var newStatus = transitionBtn.dataset.status;
      var note = prompt('Transition to "' + newStatus + '" - add a note (optional):');
      if (note === null) return;
      try {
        await api('POST', '/api/v1/si/sis/' + state.si.selectedSI.id + '/transition', { status: newStatus, note: note });
        toast('SI transitioned to ' + newStatus, 'success');
        await viewSI(state.si.selectedSI.id);
        await loadSISIs();
        renderSITable();
      } catch (err) {
        toast('Transition failed: ' + err.message, 'error');
      }
      return;
    }
  });
}

function closeSIDetail() {
  state.si.selectedSI = null;
  var detailEl = $('#si-detail');
  if (detailEl) detailEl.classList.add('hidden');
}

function bindSIEvents() {
  if (siEventsBound) return;
  siEventsBound = true;

  var customerList = $('#si-customer-list');
  if (customerList && !customerListBound) {
    customerListBound = true;
    customerList.addEventListener('click', async function(e) {
      var item = e.target.closest('.si-customer-item');
      if (!item) return;
      var id = Number(item.dataset.id);
      state.si.selectedCustomer = null;
      for (var i = 0; i < state.si.customers.length; i++) {
        if (state.si.customers[i].id === id) { state.si.selectedCustomer = state.si.customers[i]; break; }
      }
      state.si.selectedProject = null;
      renderSICustomerList();
      await loadSIProjects();
      await loadSISIs();
      renderSIFilters();
      renderSITable();
    });
  }

  var statusFilter = $('#si-status-filter');
  if (statusFilter) {
    statusFilter.addEventListener('change', async function(e) {
      state.si.filter = e.target.value;
      await loadSISIs();
      renderSITable();
    });
  }

  var projectFilter = $('#si-project-filter');
  if (projectFilter) {
    projectFilter.addEventListener('change', async function(e) {
      var id = e.target.value ? Number(e.target.value) : null;
      state.si.selectedProject = null;
      if (id) {
        for (var i = 0; i < state.si.projects.length; i++) {
          if (state.si.projects[i].id === id) { state.si.selectedProject = state.si.projects[i]; break; }
        }
      }
      await loadSISIs();
      renderSITable();
    });
  }

  var refreshBtn = $('#si-refresh-btn');
  if (refreshBtn) {
    refreshBtn.addEventListener('click', async function() {
      await loadSICustomers();
      await loadSIProjects();
      await loadSISIs();
      renderSICustomerList();
      renderSITable();
      renderSIFilters();
      toast('Refreshed', 'success');
    });
  }

  var newBtn = $('#si-new-btn');
  if (newBtn) newBtn.addEventListener('click', function() { openSICreateModal(); });

  var newCustomerBtn = $('#si-new-customer-btn');
  if (newCustomerBtn) newCustomerBtn.addEventListener('click', function() { openSICreateCustomerModal(); });

  var newProjectBtn = $('#si-new-project-btn');
  if (newProjectBtn) newProjectBtn.addEventListener('click', function() { openSICreateProjectModal(); });

  var seedBtn = $('#si-seed-btn');
  if (seedBtn) {
    seedBtn.addEventListener('click', async function() {
      try {
        var data = await api('POST', '/api/v1/si/seed', {});
        toast('Seeded ' + data.seeded + ' demo items (customer + SIs)', 'success');
        await loadSIView();
      } catch (err) {
        toast('Seed failed: ' + err.message, 'error');
      }
    });
  }

  var table = $('#si-table');
  if (table) {
    table.addEventListener('click', function(e) {
      var projectLink = e.target.closest('.si-project-link');
      if (projectLink) {
        var projectId = projectLink.dataset.projectId;
        if (projectId) {
          e.stopPropagation();
          openProjectChecklistModal(Number(projectId));
          return;
        }
      }

      var row = e.target.closest('.si-row-clickable');
      if (row) {
        viewSI(Number(row.dataset.id));
        return;
      }

      var viewBtn = e.target.closest('.si-view-btn');
      if (viewBtn) {
        viewSI(Number(viewBtn.dataset.id));
      }
    });
  }
}

async function openSICreateModal() {
  if (!state.si.projects || state.si.projects.length === 0) {
    toast('Create a customer and project first.', 'error');
    return;
  }
  var projectOptions = [];
  for (var i = 0; i < state.si.projects.length; i++) {
    var p = state.si.projects[i];
    projectOptions.push({ value: String(p.id), label: p.code + ' - ' + p.name });
  }
  var envOptions = [];
  for (var i = 0; i < SI_ENVIRONMENTS.length; i++) {
    envOptions.push({ value: SI_ENVIRONMENTS[i].value, label: SI_ENVIRONMENTS[i].label });
  }
  var result = await modal({
    title: 'New System Integration',
    fields: [
      { name: 'code', label: 'SI Code', required: true, autofocus: true, placeholder: 'SI-001' },
      { name: 'name', label: 'Name', required: true, placeholder: 'Integration name' },
      { name: 'description', label: 'Description', placeholder: 'Optional description' },
      { name: 'primaryProjectId', label: 'Primary Project', type: 'select', options: projectOptions, required: true },
      { name: 'environment', label: 'Environment', type: 'select', options: envOptions, required: true },
      { name: 'workInstructions', label: 'Work Instructions (Werkinstructies)', placeholder: 'Standard work instructions' },
      { name: 'swi', label: 'SWI', placeholder: 'Which SWI is shown' },
      { name: 'workflow', label: 'Workflow', placeholder: 'Steps to execute' },
      { name: 'qcProfile', label: 'QC Profile', placeholder: 'Quality controls needed' },
      { name: 'automations', label: 'Automations', placeholder: 'External actions' },
    ],
    confirmLabel: 'Create'
  });
  if (!result) return;
  try {
    var config = {
      workInstructions: result.workInstructions || '',
      swi: result.swi || '',
      workflow: result.workflow || '',
      qcProfile: result.qcProfile || '',
      automations: result.automations || '',
      escalationFlow: [],
    };
    await api('POST', '/api/v1/si/sis', {
      code: result.code,
      name: result.name,
      description: result.description || '',
      primaryProjectId: Number(result.primaryProjectId),
      environment: result.environment,
      config: config
    });
    toast('SI created', 'success');
    await loadSISIs();
    renderSITable();
  } catch (err) {
    toast('Create failed: ' + err.message, 'error');
  }
}

async function openSIEditModal(si) {
  var envOptions = [];
  for (var i = 0; i < SI_ENVIRONMENTS.length; i++) {
    envOptions.push({ value: SI_ENVIRONMENTS[i].value, label: SI_ENVIRONMENTS[i].label });
  }
  var cfg = si.config || {};
  var result = await modal({
    title: 'Edit SI',
    fields: [
      { name: 'name', label: 'Name', required: true, value: si.name },
      { name: 'description', label: 'Description', value: si.description || '' },
      { name: 'environment', label: 'Environment', type: 'select', options: envOptions, value: si.environment },
      { name: 'workInstructions', label: 'Work Instructions (Werkinstructies)', value: cfg.workInstructions || '' },
      { name: 'swi', label: 'SWI', value: cfg.swi || '' },
      { name: 'workflow', label: 'Workflow', value: cfg.workflow || '' },
      { name: 'qcProfile', label: 'QC Profile', value: cfg.qcProfile || '' },
      { name: 'automations', label: 'Automations', value: cfg.automations || '' },
    ],
    confirmLabel: 'Save'
  });
  if (!result) return;
  try {
    var body = {};
    if (result.name !== si.name) body.name = result.name;
    if (result.description !== si.description) body.description = result.description;
    if (result.environment !== si.environment) body.environment = result.environment;
    var newConfig = {
      workInstructions: result.workInstructions || '',
      swi: result.swi || '',
      workflow: result.workflow || '',
      qcProfile: result.qcProfile || '',
      automations: result.automations || '',
      escalationFlow: cfg.escalationFlow || [],
    };
    if (cfg.debitNumber) newConfig.debitNumber = cfg.debitNumber;
    if (cfg.deviceType) newConfig.deviceType = cfg.deviceType;
    if (cfg.configId) newConfig.configId = cfg.configId;
    if (cfg.windowsProfile) newConfig.windowsProfile = cfg.windowsProfile;
    if (cfg.software) newConfig.software = cfg.software;
    newConfig.assetSticker = cfg.assetSticker || false;
    newConfig.sleeve = cfg.sleeve || false;
    newConfig.screenProtector = cfg.screenProtector || false;
    if (cfg.otherDemands) newConfig.otherDemands = cfg.otherDemands;
    body.config = newConfig;
    await api('POST', '/api/v1/si/sis/' + si.id, body);
    toast('SI updated', 'success');
    await viewSI(si.id);
    await loadSISIs();
    renderSITable();
  } catch (err) {
    toast('Update failed: ' + err.message, 'error');
  }
}

async function openSICreateCustomerModal() {
  var result = await modal({
    title: 'New Customer',
    fields: [
      { name: 'number', label: 'Customer Number', required: true, autofocus: true, placeholder: '94828' },
      { name: 'name', label: 'Customer Name', required: true, placeholder: 'Company name' }
    ],
    confirmLabel: 'Create'
  });
  if (!result) return;
  try {
    await api('POST', '/api/v1/si/customers', { number: result.number, name: result.name });
    toast('Customer created', 'success');
    await loadSICustomers();
    renderSICustomerList();
  } catch (err) {
    toast('Create failed: ' + err.message, 'error');
  }
}

async function openSICreateProjectModal() {
  if (!state.si.selectedCustomer) {
    toast('Select a customer first.', 'error');
    return;
  }
  var result = await modal({
    title: 'New Project',
    fields: [
      { name: 'code', label: 'Project Code', required: true, autofocus: true, placeholder: 'PRJ-001' },
      { name: 'name', label: 'Project Name', required: true, placeholder: 'Project name' },
      { name: 'description', label: 'Description', placeholder: 'Optional description' }
    ],
    confirmLabel: 'Create'
  });
  if (!result) return;
  try {
    await api('POST', '/api/v1/si/projects', {
      customerId: state.si.selectedCustomer.id,
      code: result.code,
      name: result.name,
      description: result.description || ''
    });
    toast('Project created', 'success');
    await loadSIProjects();
    await loadSISIs();
    renderSIFilters();
    renderSITable();
  } catch (err) {
    toast('Create failed: ' + err.message, 'error');
  }
}


async function openSIManageProjectsModal(si) {
  if (!state.si.projects || state.si.projects.length === 0) {
    toast('No projects available. Create a project first.', 'error');
    return;
  }
  var currentIds = si.projectIds || [];
  var fields = [];
  for (var i = 0; i < state.si.projects.length; i++) {
    var p = state.si.projects[i];
    fields.push({
      name: 'project_' + p.id,
      label: p.code + ' - ' + p.name,
      type: 'checkbox',
      checked: currentIds.indexOf(p.id) !== -1
    });
  }
  var result = await modal({
    title: 'Manage Linked Projects',
    fields: fields,
    confirmLabel: 'Save'
  });
  if (!result) return;
  var newIds = [];
  for (var i = 0; i < state.si.projects.length; i++) {
    var p = state.si.projects[i];
    if (result['project_' + p.id] === 'on' || result['project_' + p.id] === true) {
      newIds.push(p.id);
    }
  }
  try {
    await api('POST', '/api/v1/si/sis/' + si.id + '/projects', { projectIds: newIds });
    toast('Projects updated', 'success');
    await viewSI(si.id);
    await loadSISIs();
    renderSITable();
  } catch (err) {
    toast('Update failed: ' + err.message, 'error');
  }
}

function updateSINavVisibility() {
  var navSI = $('#nav-si');
  if (navSI) {
    if (can('si:list')) navSI.classList.remove('hidden');
    else navSI.classList.add('hidden');
  }
}


async function openProjectChecklistModal(projectId) {
  var project = state.si.projects.find(function(p) { return p.id === projectId; });
  if (!project) {
    toast('Select a project first.', 'error');
    return;
  }
  state.si.checklistProjectId = projectId;
  await loadProjectChecklist(projectId);
  renderChecklistModal(project);
}

async function loadProjectChecklist(projectId) {
  try {
    var data = await api('GET', '/api/v1/si/projects/' + projectId + '/checklist');
    state.si.checklist = data.items || [];
  } catch (err) {
    toast('Failed to load checklist: ' + err.message, 'error');
    state.si.checklist = [];
  }
}

function renderChecklistModal(project) {
  var items = state.si.checklist || [];
  var canEdit = can('si:update');
  var itemsHtml = '';
  if (items.length === 0) {
    itemsHtml = '<p class="muted">No checklist items yet. Add one below.</p>';
  } else {
    itemsHtml = '<div class="checklist-items">';
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      var checkedClass = item.checkedBy ? ' checked' : '';
      var checkinUrl = location.origin + '/checkin.html?p=' + project.id + '&i=' + item.id;
      itemsHtml += '<div class="checklist-item' + checkedClass + '" data-id="' + item.id + '">' +
        '<div class="checklist-item-main">' +
        '<div class="checklist-item-label">' + escapeSI(item.label) + '</div>' +
        (item.description ? '<div class="checklist-item-desc">' + escapeSI(item.description) + '</div>' : '') +
        (item.checkedBy ? '<div class="checklist-item-meta">✓ ' + escapeSI(item.checkedBy) + ' · ' + fmtTime(item.checkedAt) + '</div>' : '') +
        '</div>' +
        '<div class="checklist-item-actions">' +
        '<button type="button" class="btn btn-sm btn-ghost si-qr-btn" data-url="' + checkinUrl + '" title="Show QR">QR</button>' +
        (canEdit ? (item.checkedBy ?
          '<button type="button" class="btn btn-sm btn-ghost si-uncheck-btn" data-id="' + item.id + '">Undo</button>' :
          '<button type="button" class="btn btn-sm btn-ghost si-check-btn" data-id="' + item.id + '">✓</button>' +
          '<button type="button" class="btn btn-sm btn-ghost si-del-item-btn" data-id="' + item.id + '">✕</button>') : '') +
        '</div></div>';
    }
    itemsHtml += '</div>';
  }

  var addHtml = '';
  if (canEdit) {
    addHtml = '<div class="checklist-add-form">' +
      '<input type="text" id="checklist-new-label" placeholder="New item..." />' +
      '<button type="button" class="btn btn-sm btn-primary" id="checklist-add-btn">Add</button>' +
      '</div>';
  }

  var html = '<div class="checklist-modal-content">' +
    '<h3>' + escapeSI(project.code) + ' — ' + escapeSI(project.name) + '</h3>' +
    itemsHtml +
    addHtml +
    '</div>';

  var result = modal({
    title: 'Project Checklist',
    bodyHtml: html,
    confirmLabel: 'Close',
    wide: true
  });

  setTimeout(function() {
    bindChecklistModalEvents(project.id);
  }, 50);
}

function bindChecklistModalEvents(projectId) {
  var modalBody = $('.modal-body');
  if (!modalBody) return;

  if (modalBody._checklistHandler) {
    modalBody.removeEventListener('click', modalBody._checklistHandler);
  }

  modalBody._checklistHandler = async function(e) {
    var target = e.target;

    if (target.id === 'checklist-add-btn' || target.closest('#checklist-add-btn')) {
      var input = $('#checklist-new-label');
      var label = input.value.trim();
      if (!label) return;
      try {
        await api('POST', '/api/v1/si/projects/' + projectId + '/checklist', { label: label, description: '' });
        await loadProjectChecklist(projectId);
        var project = state.si.projects.find(function(p) { return p.id === projectId; });
        renderChecklistModal(project);
      } catch (err) {
        toast('Add failed: ' + err.message, 'error');
      }
      return;
    }

    var checkBtn = target.closest('.si-check-btn');
    if (checkBtn) {
      var itemId = checkBtn.dataset.id;
      try {
        await api('POST', '/api/v1/si/projects/checklist/' + itemId + '/tick', {});
        await loadProjectChecklist(projectId);
        var project = state.si.projects.find(function(p) { return p.id === projectId; });
        renderChecklistModal(project);
      } catch (err) {
        toast('Check failed: ' + err.message, 'error');
      }
      return;
    }

    var uncheckBtn = target.closest('.si-uncheck-btn');
    if (uncheckBtn) {
      var itemId = uncheckBtn.dataset.id;
      try {
        await api('POST', '/api/v1/si/projects/checklist/' + itemId + '/untick', {});
        await loadProjectChecklist(projectId);
        var project = state.si.projects.find(function(p) { return p.id === projectId; });
        renderChecklistModal(project);
      } catch (err) {
        toast('Undo failed: ' + err.message, 'error');
      }
      return;
    }

    var delBtn = target.closest('.si-del-item-btn');
    if (delBtn) {
      var itemId = delBtn.dataset.id;
      try {
        await api('DELETE', '/api/v1/si/projects/checklist/' + itemId);
        await loadProjectChecklist(projectId);
        var project = state.si.projects.find(function(p) { return p.id === projectId; });
        renderChecklistModal(project);
      } catch (err) {
        toast('Delete failed: ' + err.message, 'error');
      }
      return;
    }

    var qrBtn = target.closest('.si-qr-btn');
    if (qrBtn) {
      showQRCode(qrBtn.dataset.url);
      return;
    }
  };

  modalBody.addEventListener('click', modalBody._checklistHandler);
}

function showQRCode(url) {
  var qrHtml = '<div class="qr-modal-content">' +
    '<h3>Scan to Check In</h3>' +
    '<div class="qr-code-container" id="qr-code-canvas"></div>' +
    '<p class="muted qr-url">' + escapeSI(url) + '</p>' +
    '<button type="button" class="btn btn-secondary" id="qr-print-btn">Print</button>' +
    '</div>';
  modal({
    title: 'QR Code',
    bodyHtml: qrHtml,
    confirmLabel: 'Close'
  });
  setTimeout(function() {
    generateQRCode('qr-code-canvas', url);
    var printBtn = $('#qr-print-btn');
    if (printBtn) {
      printBtn.addEventListener('click', function() {
        var canvas = $('#qr-code-canvas canvas');
        if (canvas) {
          var win = window.open('', '_blank');
          win.document.write('<html><head><title>QR Code</title></head><body style="text-align:center;padding:40px;">' +
            '<img src="' + canvas.toDataURL() + '" />' +
            '<p>' + url + '</p>' +
            '</body></html>');
          win.document.close();
          win.print();
        }
      });
    }
  }, 50);
}

function generateQRCode(containerId, text) {
  var container = $('#' + containerId);
  if (!container) return;
  container.innerHTML = '';
  if (typeof QRCode !== 'undefined') {
    new QRCode(container, {
      text: text,
      width: 256,
      height: 256,
      colorDark: '#000000',
      colorLight: '#ffffff',
      correctLevel: QRCode.CorrectLevel.M
    });
  } else {
    container.innerHTML = '<p class="muted">QR Code library not loaded.</p>';
  }
}
