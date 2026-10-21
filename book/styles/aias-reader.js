/* Aias Reader 1.0. Shared preference across books on aias00.github.io. */
(() => {
  'use strict';
  const root = document.documentElement;
  const key = 'aias-reader-theme';
  const read = () => { try { return localStorage.getItem(key); } catch { return null; } };
  let theme = read() === 'dark' ? 'dark' : 'light';
  let control;
  function apply() {
    root.dataset.theme = theme;
    if (document.body) document.body.classList.toggle('dark', theme === 'dark');
    const palette = document.querySelector('[data-md-component="palette"]');
    if (palette) {
      const scheme = theme === 'dark' ? 'slate' : 'default';
      document.body.dataset.mdColorScheme = scheme;
      const selected = palette.querySelector(`input[data-md-color-scheme="${scheme}"]`);
      if (selected) {
        selected.checked = true;
        palette.querySelectorAll('label[for]').forEach(label => { label.hidden = label.htmlFor === selected.id; });
      }
    }
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#1c1f24' : '#f7f7f3');
    if (!control) return;
    if (control.tagName === 'SELECT') {
      control.value = theme;
    } else {
      control.textContent = theme === 'dark' ? '切换浅色' : '切换深色';
      control.setAttribute('aria-pressed', String(theme === 'dark'));
    }
    control.setAttribute('aria-label', theme === 'dark' ? '切换到浅色模式' : '切换到深色模式');
  }
  apply();
  function initialize() {
    const palette = document.querySelector('[data-md-component="palette"]');
    if (palette) {
      palette.addEventListener('change', event => {
        theme = event.target.dataset.mdColorScheme === 'slate' ? 'dark' : 'light';
        try { localStorage.setItem(key, theme); } catch { /* Optional persistence. */ }
        apply();
      });
      apply();
      return;
    }
    control = document.querySelector('#theme-toggle, #theme, select[name="theme"]');
    if (!control) {
      const bar = document.createElement('div');
      bar.className = 'aias-reader-tools';
      const home = document.createElement('a');
      home.href = 'https://aias00.github.io/#books';
      home.textContent = 'Aias · 电子书架';
      control = document.createElement('button');
      control.type = 'button';
      bar.append(home, control);
      (document.querySelector('main') || document.body).prepend(bar);
    }
    control.hidden = false;
    control.classList.add('aias-reader-theme');
    if (control.tagName === 'SELECT') {
      control.replaceChildren(...['light', 'dark'].map(value => {
        const option = document.createElement('option');
        option.value = value;
        option.textContent = value === 'light' ? '浅色' : '深色';
        return option;
      }));
    }
    const eventName = control.tagName === 'SELECT' ? 'change' : 'click';
    // Capture before legacy handlers; all other reader controls keep working.
    document.addEventListener(eventName, event => {
      if (!control.contains(event.target)) return;
      event.stopImmediatePropagation();
      if (eventName === 'click') event.preventDefault();
      theme = control.tagName === 'SELECT' ? control.value : theme === 'dark' ? 'light' : 'dark';
      try { localStorage.setItem(key, theme); } catch { /* Reading works without storage. */ }
      apply();
    }, true);
    apply();
  }
  // Legacy initializers finish first; the shared preference is applied last.
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => setTimeout(initialize, 0), { once: true });
  } else {
    setTimeout(initialize, 0);
  }
  addEventListener('storage', event => {
    if (event.key !== key && event.key !== null) return;
    theme = event.newValue === 'dark' ? 'dark' : 'light';
    apply();
  });
})();
