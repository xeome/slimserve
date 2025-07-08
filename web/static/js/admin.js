// SlimServe Admin JavaScript

// Admin utility functions
window.adminUtils = {
    // Format file sizes
    formatFileSize(bytes) {
        if (bytes === 0) return '0 Bytes';
        const k = 1024;
        const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    },

    // Format dates
    formatDate(dateString) {
        const date = new Date(dateString);
        return date.toLocaleDateString() + ' ' + date.toLocaleTimeString();
    },

    // Get CSRF token
    getCSRFToken() {
        return document.querySelector('meta[name="csrf-token"]')?.getAttribute('content') || 
               document.querySelector('input[name="csrf_token"]')?.value;
    },

    // Make authenticated API request
    async apiRequest(url, options = {}) {
        const csrfToken = this.getCSRFToken();
        
        const defaultOptions = {
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': csrfToken,
                ...options.headers
            }
        };

        if (options.body && typeof options.body === 'object' && !(options.body instanceof FormData)) {
            defaultOptions.body = JSON.stringify(options.body);
        }

        const response = await fetch(url, { ...defaultOptions, ...options });
        
        if (!response.ok) {
            const error = await response.json().catch(() => ({ error: 'Request failed' }));
            throw new Error(error.error || `HTTP ${response.status}`);
        }

        return response.json();
    },

    // Show notification
    showNotification(message, type = 'info') {
        // Create notification element
        const notification = document.createElement('div');
        notification.className = `fixed top-4 right-4 z-50 p-4 rounded-lg shadow-lg max-w-sm transition-all duration-300 transform translate-x-full`;
        
        // Set colors based on type
        const colors = {
            success: 'bg-green-500 text-white',
            error: 'bg-red-500 text-white',
            warning: 'bg-yellow-500 text-black',
            info: 'bg-blue-500 text-white'
        };
        
        notification.className += ` ${colors[type] || colors.info}`;
        notification.textContent = message;
        
        document.body.appendChild(notification);
        
        // Animate in
        setTimeout(() => {
            notification.classList.remove('translate-x-full');
        }, 100);
        
        // Auto remove after 5 seconds
        setTimeout(() => {
            notification.classList.add('translate-x-full');
            setTimeout(() => {
                document.body.removeChild(notification);
            }, 300);
        }, 5000);
    },

    // Confirm dialog
    async confirm(message, title = 'Confirm') {
        return new Promise((resolve) => {
            const modal = document.createElement('div');
            modal.className = 'fixed inset-0 z-50 flex items-center justify-center bg-black bg-opacity-50';
            modal.innerHTML = `
                <div class="bg-card border border-border rounded-lg p-6 max-w-md w-full mx-4">
                    <h3 class="text-lg font-semibold text-foreground mb-2">${title}</h3>
                    <p class="text-muted-foreground mb-6">${message}</p>
                    <div class="flex justify-end space-x-2">
                        <button id="cancel-btn" class="px-4 py-2 text-muted-foreground hover:text-foreground border border-border rounded-md hover:bg-muted/20 transition-colors">
                            Cancel
                        </button>
                        <button id="confirm-btn" class="px-4 py-2 bg-destructive text-destructive-foreground rounded-md hover:bg-destructive/90 transition-colors">
                            Confirm
                        </button>
                    </div>
                </div>
            `;
            
            document.body.appendChild(modal);
            
            modal.querySelector('#cancel-btn').onclick = () => {
                document.body.removeChild(modal);
                resolve(false);
            };
            
            modal.querySelector('#confirm-btn').onclick = () => {
                document.body.removeChild(modal);
                resolve(true);
            };
            
            // Close on backdrop click
            modal.onclick = (e) => {
                if (e.target === modal) {
                    document.body.removeChild(modal);
                    resolve(false);
                }
            };
        });
    }
};

// Admin file manager component
window.adminFileManager = function() {
    return {
        files: [],
        currentPath: '/',
        loading: false,
        selectedFiles: new Set(),
        
        init() {
            this.loadFiles();
        },
        
        async loadFiles(path = this.currentPath) {
            this.loading = true;
            try {
                const data = await adminUtils.apiRequest(`/admin/api/files?path=${encodeURIComponent(path)}`);
                this.files = data.files || [];
                this.currentPath = path;
            } catch (error) {
                adminUtils.showNotification('Failed to load files: ' + error.message, 'error');
            } finally {
                this.loading = false;
            }
        },
        
        async deleteFile(filename) {
            const confirmed = await adminUtils.confirm(
                `Are you sure you want to delete "${filename}"?`,
                'Delete File'
            );
            
            if (!confirmed) return;
            
            try {
                await adminUtils.apiRequest('/admin/api/files/delete', {
                    method: 'POST',
                    body: {
                        path: this.currentPath,
                        filename: filename
                    }
                });
                
                adminUtils.showNotification('File deleted successfully', 'success');
                this.loadFiles();
            } catch (error) {
                adminUtils.showNotification('Failed to delete file: ' + error.message, 'error');
            }
        },
        
        async createFolder(name) {
            if (!name) return;
            
            try {
                await adminUtils.apiRequest('/admin/api/files/mkdir', {
                    method: 'POST',
                    body: {
                        path: this.currentPath,
                        name: name
                    }
                });
                
                adminUtils.showNotification('Folder created successfully', 'success');
                this.loadFiles();
            } catch (error) {
                adminUtils.showNotification('Failed to create folder: ' + error.message, 'error');
            }
        },
        
        toggleFileSelection(filename) {
            if (this.selectedFiles.has(filename)) {
                this.selectedFiles.delete(filename);
            } else {
                this.selectedFiles.add(filename);
            }
        },
        
        selectAllFiles() {
            this.files.forEach(file => {
                if (!file.isDir) {
                    this.selectedFiles.add(file.name);
                }
            });
        },
        
        clearSelection() {
            this.selectedFiles.clear();
        },
        
        async deleteSelected() {
            if (this.selectedFiles.size === 0) return;
            
            const confirmed = await adminUtils.confirm(
                `Are you sure you want to delete ${this.selectedFiles.size} selected file(s)?`,
                'Delete Files'
            );
            
            if (!confirmed) return;
            
            const promises = Array.from(this.selectedFiles).map(filename => 
                this.deleteFile(filename)
            );
            
            await Promise.all(promises);
            this.clearSelection();
        }
    };
};

// Admin configuration manager
window.adminConfig = function() {
    return {
        config: {},
        loading: false,
        saving: false,
        
        init() {
            this.loadConfig();
        },
        
        async loadConfig() {
            this.loading = true;
            try {
                this.config = await adminUtils.apiRequest('/admin/api/config');
            } catch (error) {
                adminUtils.showNotification('Failed to load configuration: ' + error.message, 'error');
            } finally {
                this.loading = false;
            }
        },
        
        async saveConfig() {
            this.saving = true;
            try {
                await adminUtils.apiRequest('/admin/api/config', {
                    method: 'POST',
                    body: this.config
                });
                
                adminUtils.showNotification('Configuration saved successfully', 'success');
            } catch (error) {
                adminUtils.showNotification('Failed to save configuration: ' + error.message, 'error');
            } finally {
                this.saving = false;
            }
        }
    };
};

// Admin system status monitor
window.adminStatus = function() {
    return {
        status: {},
        loading: false,
        autoRefresh: true,
        refreshInterval: null,
        
        init() {
            this.loadStatus();
            if (this.autoRefresh) {
                this.startAutoRefresh();
            }
        },
        
        async loadStatus() {
            this.loading = true;
            try {
                this.status = await adminUtils.apiRequest('/admin/api/status');
            } catch (error) {
                adminUtils.showNotification('Failed to load system status: ' + error.message, 'error');
            } finally {
                this.loading = false;
            }
        },
        
        startAutoRefresh() {
            this.refreshInterval = setInterval(() => {
                if (this.autoRefresh) {
                    this.loadStatus();
                }
            }, 30000); // Refresh every 30 seconds
        },
        
        stopAutoRefresh() {
            if (this.refreshInterval) {
                clearInterval(this.refreshInterval);
                this.refreshInterval = null;
            }
        },
        
        toggleAutoRefresh() {
            this.autoRefresh = !this.autoRefresh;
            if (this.autoRefresh) {
                this.startAutoRefresh();
            } else {
                this.stopAutoRefresh();
            }
        }
    };
};

// Initialize admin theme handling
document.addEventListener('DOMContentLoaded', function() {
    // Theme toggle functionality
    const themeToggle = document.querySelector('#theme-toggle');
    if (themeToggle) {
        themeToggle.addEventListener('click', function() {
            const html = document.documentElement;
            const currentTheme = html.getAttribute('data-theme');
            const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
            
            html.setAttribute('data-theme', newTheme);
            localStorage.setItem('slimserve-admin-theme', newTheme);
        });
    }
    
    // Load saved theme
    const savedTheme = localStorage.getItem('slimserve-admin-theme');
    if (savedTheme) {
        document.documentElement.setAttribute('data-theme', savedTheme);
    }
});
