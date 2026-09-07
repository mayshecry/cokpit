'use strict';

(function() {
  var params = new URLSearchParams(location.search);
  var barcode = params.get('code');
  var token = localStorage.getItem('cockpit_token');
  
  function $(id) { return document.getElementById(id); }
  
  function escapeHtml(str) {
    return String(str || '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }
  
  function showError(msg) {
    $('scan-content').innerHTML = '<div class="scan-status"><p class="error">' + escapeHtml(msg) + '</p>' +
      '<a class="btn btn-secondary" href="/">Go to Dashboard</a></div>';
  }
  
  function renderOrder(order, omniUrl) {
    var html = '<h1>Order #' + escapeHtml(order.orderNumber) + '</h1>' +
      '<p class="subtitle">Scanned via barcode</p>' +
      '<div class="barcode-display"><span class="code">' + escapeHtml(order.barcode) + '</span></div>' +
      '<div class="order-info">';
    
    var fields = [
      ['Debit Number', order.debitNumber],
      ['Customer', order.customerName],
      ['Omnitracker Ticket', order.omnitrackerTicket],
      ['Device', order.device],
      ['Asset/Serial', order.assetNumber],
      ['Configuration', order.configuration],
      ['SI', order.siId ? '#' + order.siId : null],
      ['Status', order.status]
    ];
    
    for (var i = 0; i < fields.length; i++) {
      if (fields[i][1]) {
        html += '<div class="info-row"><span class="info-label">' + escapeHtml(fields[i][0]) + '</span>' +
          '<span class="info-value">' + escapeHtml(fields[i][1]) + '</span></div>';
      }
    }
    
    html += '</div><div class="actions">';
    html += '<a class="btn btn-secondary" href="/#/orders/' + order.id + '">Open in Cockpit</a>';
    if (omniUrl) {
      html += '<a class="btn btn-primary omni-link" href="' + escapeHtml(omniUrl) + '" target="_blank">Open in Omnitracker</a>';
    }
    html += '</div>';
    
    $('scan-content').innerHTML = html;
  }
  
  async function loadOrder() {
    if (!barcode) {
      showError('No barcode provided.');
      return;
    }
    
    try {
      var res = await fetch('/api/v1/scan/barcode?code=' + encodeURIComponent(barcode), {
        headers: token ? { Authorization: 'Bearer ' + token } : {}
      });
      var data = await res.json();
      if (!res.ok) throw new Error(data.error?.message || 'Failed to load order');
      renderOrder(data.order, data.omniUrl);
    } catch (err) {
      showError(err.message);
    }
  }
  
  loadOrder();
})();