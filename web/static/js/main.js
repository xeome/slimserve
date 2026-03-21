// SlimServe Alpine.js Components

// Main UI component for file listing
window.slimserveUI = function slimserveUI() {
    return {
        view: localStorage.getItem('slimserve-view') || 'list',
        filter: localStorage.getItem('slimserve-filter') || 'all',

        init() {
            document.addEventListener('keydown', (e) => this.handleKeydown(e));
        },

        setView(newView) {
            this.view = newView;
            localStorage.setItem('slimserve-view', newView);
        },

        setFilter(newFilter) {
            this.filter = newFilter;
            localStorage.setItem('slimserve-filter', newFilter);
        },

        handleKeydown(e) {
            if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.isContentEditable) {
                return;
            }

            if (e.ctrlKey || e.metaKey || e.altKey) {
                return;
            }

            switch (e.key.toLowerCase()) {
                case 'g':
                    e.preventDefault();
                    this.setView('grid');
                    break;
                case 'l':
                    e.preventDefault();
                    this.setView('list');
                    break;
                case '1':
                    e.preventDefault();
                    this.setFilter('all');
                    break;
                case '2':
                    e.preventDefault();
                    this.setFilter('folder');
                    break;
                case '3':
                    e.preventDefault();
                    this.setFilter('image');
                    break;
                case '4':
                    e.preventDefault();
                    this.setFilter('document');
                    break;
            }
        }
    };
}

document.addEventListener('DOMContentLoaded', function () {
    const storageKey = 'slimserve-theme';
    const root = document.documentElement;
    const toggleBtn = document.getElementById('theme-toggle');

    function safeGetItem(key) {
        try { return localStorage.getItem(key); }
        catch { return null; }
    }

    function safeSetItem(key, value) {
        try { localStorage.setItem(key, value); }
        catch { }
    }

    function getPreferred() {
        return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches
            ? 'dark' : 'light';
    }

    function applyTheme(theme) {
        if (theme === 'dark') {
            root.classList.remove('light');
            root.setAttribute('data-theme', 'dark');
            toggleBtn && toggleBtn.setAttribute('aria-pressed', 'true');
        } else {
            root.classList.add('light');
            root.setAttribute('data-theme', 'light');
            toggleBtn && toggleBtn.setAttribute('aria-pressed', 'false');
        }
    }

    function initTheme() {
        if (!toggleBtn) return;
        let theme = safeGetItem(storageKey);
        if (theme !== 'light' && theme !== 'dark') {
            theme = getPreferred();
        }
        applyTheme(theme);

        toggleBtn.addEventListener('click', () => {
            const current = root.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
            const next = current === 'dark' ? 'light' : 'dark';
            safeSetItem(storageKey, next);
            applyTheme(next);
        });
    }

    initTheme();

    document.addEventListener('click', function (e) {
        const row = e.target.closest('tr[data-type]');
        if (row && row.getAttribute('data-type') !== 'folder') {
            if (e.ctrlKey || e.metaKey) {
                e.preventDefault();
                const url = row.getAttribute('onclick')?.match(/'([^']+)'/)?.[1];
                if (url) {
                    const link = document.createElement('a');
                    link.href = url;
                    link.download = '';
                    document.body.appendChild(link);
                    link.click();
                    document.body.removeChild(link);
                }
            }
        }
    });

    const shortcuts = document.createElement('div');
    shortcuts.style.cssText = 'position:fixed;bottom:20px;right:20px;background:var(--color-background);border:1px solid var(--color-border);border-radius:8px;padding:12px;font-size:12px;color:var(--color-muted-foreground);z-index:40;display:none;';
    shortcuts.innerHTML = `
        <div style="margin-bottom:8px;font-weight:500;">Keyboard Shortcuts:</div>
        <div>G - Grid view</div>
        <div>L - List view</div>
        <div>1 - All files</div>
        <div>2 - Folders only</div>
        <div>3 - Images only</div>
        <div>4 - Documents only</div>
        <div style="margin-top:8px;font-size:11px;">Ctrl+Click file to download</div>
    `;
    document.body.appendChild(shortcuts);

    document.addEventListener('keydown', function (e) {
        if (e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey) {
            e.preventDefault();
            shortcuts.style.display = shortcuts.style.display === 'none' ? 'block' : 'none';
        }
        if (e.key === 'Escape') {
            shortcuts.style.display = 'none';
        }
    });
});

window.loginForm = function () {
    return {
        loading: false,
        passwordVisible: false,
        togglePassword() {
            this.passwordVisible = !this.passwordVisible;
            this.$refs.password.type = this.passwordVisible ? 'text' : 'password';
        },
        trapFocus(e) {
            const focusableElements = this.$el.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
            const firstFocusable = focusableElements[0];
            const lastFocusable = focusableElements[focusableElements.length - 1];

            if (e.key === 'Tab') {
                if (e.shiftKey) {
                    if (document.activeElement === firstFocusable) {
                        lastFocusable.focus();
                        e.preventDefault();
                    }
                } else {
                    if (document.activeElement === lastFocusable) {
                        firstFocusable.focus();
                        e.preventDefault();
                    }
                }
            }
        },
        init() {
            this.$nextTick(() => {
                this.$refs.username && this.$refs.username.focus();
            });
        }
    };
}