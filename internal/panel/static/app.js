/* Mini Panel — UI interactions (no inline scripts, CSP friendly) */
(function () {
  'use strict';

  function onReady(fn) {
    if (document.readyState !== 'loading') fn();
    else document.addEventListener('DOMContentLoaded', fn);
  }

  onReady(function () {
    initSidebar();
    initActiveNav();
    initDeployForm();
    initConfirmForms();
  });

  /* Sidebar toggle (mobile) */
  function initSidebar() {
    var btn = document.getElementById('menu-btn');
    var scrim = document.getElementById('side-scrim');
    if (!btn) return;
    btn.addEventListener('click', function () {
      document.body.classList.toggle('nav-open');
    });
    if (scrim) {
      scrim.addEventListener('click', function () {
        document.body.classList.remove('nav-open');
      });
    }
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape') document.body.classList.remove('nav-open');
    });
  }

  /* Highlight current section in the sidebar */
  function initActiveNav() {
    var path = window.location.pathname || '/';
    var links = document.querySelectorAll('.side-link[data-nav]');
    var best = null;
    var bestLen = -1;
    links.forEach(function (link) {
      var prefix = link.getAttribute('data-nav');
      if (path === prefix || path.indexOf(prefix + '/') === 0) {
        if (prefix.length > bestLen) { bestLen = prefix.length; best = link; }
      }
    });
    if (best) best.classList.add('active');
  }

  /* Deploy form: show fields matching runtime+source (domains page) */
  function initDeployForm() {
    var runtime = document.getElementById('runtime');
    var source = document.getElementById('source');
    var deployType = document.getElementById('deploy_type');
    if (!runtime || !source || !deployType) return;

    var sections = Array.prototype.slice.call(document.querySelectorAll('[data-deploy-fields]'));
    var sources = {
      static: [['blank', 'Blank Site'], ['upload', 'Upload ZIP']],
      php: [['upload', 'Upload ZIP'], ['git', 'Git Repository']],
      node: [['git', 'Git Repository'], ['upload', 'Upload ZIP']],
      go: [['binary', 'Binary Upload']],
      proxy: [['port', 'Existing Port']]
    };
    var deployMap = {
      'static:blank': 'static_site',
      'static:upload': 'static_zip',
      'php:upload': 'php_zip',
      'php:git': 'php_git',
      'node:git': 'node_git',
      'node:upload': 'node_zip',
      'go:binary': 'go_binary',
      'proxy:port': 'reverse_proxy'
    };

    function sync() {
      var previous = source.value;
      var nextSources = sources[runtime.value] || [];
      while (source.firstChild) source.removeChild(source.firstChild);
      nextSources.forEach(function (pair) {
        var option = document.createElement('option');
        option.value = pair[0];
        option.textContent = pair[1];
        source.appendChild(option);
      });
      var stillThere = nextSources.some(function (pair) { return pair[0] === previous; });
      if (stillThere) source.value = previous;

      var activeType = deployMap[runtime.value + ':' + source.value] || 'static_site';
      deployType.value = activeType;
      sections.forEach(function (section) {
        var active = section.getAttribute('data-deploy-fields') === activeType;
        section.hidden = !active;
        var fields = section.querySelectorAll('input, select, textarea');
        fields.forEach(function (field) { field.disabled = !active; });
      });
    }

    runtime.addEventListener('change', sync);
    source.addEventListener('change', sync);
    sync();
  }

  /* Confirm destructive actions */
  function initConfirmForms() {
    document.querySelectorAll('form[data-confirm]').forEach(function (form) {
      form.addEventListener('submit', function (e) {
        var msg = form.getAttribute('data-confirm') || 'Yakin ingin melanjutkan?';
        if (!window.confirm(msg)) e.preventDefault();
      });
    });
  }
})();
