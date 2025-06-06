// SlimServe UI Enhancements
document.addEventListener('DOMContentLoaded', function () {
    // Theme Toggle
    const themeToggle = document.getElementById('theme-toggle');
    const themeIconDark = document.getElementById('theme-icon-dark');
    const themeIconLight = document.getElementById('theme-icon-light');
    const htmlElement = document.documentElement;

    // Initialize theme from localStorage or default to dark
    const savedTheme = localStorage.getItem('slimserve-theme') || 'dark';
    setTheme(savedTheme);

    themeToggle.addEventListener('click', function () {
        const currentTheme = htmlElement.getAttribute('data-mode');
        const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
        setTheme(newTheme);
        localStorage.setItem('slimserve-theme', newTheme);
    });

    function setTheme(theme) {
        htmlElement.setAttribute('data-mode', theme);
        if (theme === 'dark') {
            htmlElement.classList.remove('light');
            themeIconDark.classList.add('hidden');
            themeIconLight.classList.remove('hidden');
        } else {
            htmlElement.classList.add('light');
            themeIconDark.classList.remove('hidden');
            themeIconLight.classList.add('hidden');
        }
    }

    // View Switcher
    const fileContainer = document.querySelector('.file-container');
    const viewTabs = document.querySelectorAll('[data-view]');

    viewTabs.forEach(tab => {
        tab.addEventListener('click', function (e) {
            e.preventDefault();

            // Update active tab
            viewTabs.forEach(t => {
                t.parentElement.classList.remove('uk-active');
                t.classList.remove('bg-primary', 'text-primary-foreground');
                t.classList.add('text-muted-foreground');
            });
            this.parentElement.classList.add('uk-active');
            this.classList.add('bg-primary', 'text-primary-foreground');
            this.classList.remove('text-muted-foreground');

            // Toggle views
            if (this.dataset.view === 'grid') {
                fileContainer.classList.remove('list-view');
                fileContainer.classList.add('grid-view');
                localStorage.setItem('slimserve-view', 'grid');
            } else {
                fileContainer.classList.remove('grid-view');
                fileContainer.classList.add('list-view');
                localStorage.setItem('slimserve-view', 'list');
            }
        });
    });

    // Restore saved view preference
    const savedView = localStorage.getItem('slimserve-view') || 'list';
    const activeTab = document.querySelector(`[data-view="${savedView}"]`);
    if (activeTab) {
        activeTab.click();
    }

    // File Type Filtering
    const filterButtons = document.querySelectorAll('[data-uk-filter-control]');
    const fileItems = document.querySelectorAll('[data-type]');

    filterButtons.forEach(button => {
        button.addEventListener('click', function (e) {
            e.preventDefault();

            // Update active filter button - force immediate visual changes
            filterButtons.forEach(btn => {
                btn.classList.remove('uk-btn-primary');
                btn.classList.add('uk-btn-default');
                // Force repaint
                btn.offsetHeight;
            });
            this.classList.remove('uk-btn-default');
            this.classList.add('uk-btn-primary');
            // Force repaint
            this.offsetHeight;

            // Get filter criteria
            const filterAttr = this.getAttribute('data-uk-filter-control');

            fileItems.forEach(item => {
                if (!filterAttr || filterAttr === '' || item.matches(filterAttr)) {
                    item.style.display = '';
                } else {
                    item.style.display = 'none';
                }
            });
        });
    });

    // Keyboard shortcuts
    document.addEventListener('keydown', function (e) {
        // Only trigger if not in an input field
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') {
            return;
        }

        if (e.key === 'g' && !e.ctrlKey && !e.metaKey) {
            e.preventDefault();
            const gridTab = document.querySelector('[data-view="grid"]');
            if (gridTab) {
                gridTab.click();
            }
        } else if (e.key === 'l' && !e.ctrlKey && !e.metaKey) {
            e.preventDefault();
            const listTab = document.querySelector('[data-view="list"]');
            if (listTab) {
                listTab.click();
            }
        }
    });

    // Add loading states for better UX (exclude filter buttons and view toggles)
    document.querySelectorAll('a[href]:not([data-uk-filter-control]):not([data-view])').forEach(link => {
        link.addEventListener('click', function () {
            this.style.opacity = '0.6';
            this.style.pointerEvents = 'none';
            setTimeout(() => {
                this.style.opacity = '';
                this.style.pointerEvents = '';
            }, 2000);
        });
    });
});