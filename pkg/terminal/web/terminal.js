let terminal = null;
let fitAddon = null;
let socket = null;
let currentFork = null;

// Initialize terminal
function initTerminal() {
    terminal = new Terminal({
        rows: 40,
        cols: 140,
        cursorBlink: true,
        fontSize: 14,
        fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        theme: {
            background: '#1e1e1e',
            foreground: '#d4d4d4',
            cursor: '#aeafad',
            black: '#000000',
            red: '#cd3131',
            green: '#0dbc79',
            yellow: '#e5e510',
            blue: '#2472c8',
            magenta: '#bc3fbc',
            cyan: '#11a8cd',
            white: '#e5e5e5',
            brightBlack: '#666666',
            brightRed: '#f14c4c',
            brightGreen: '#23d18b',
            brightYellow: '#f5f543',
            brightBlue: '#3b8eea',
            brightMagenta: '#d670d6',
            brightCyan: '#29b8db',
            brightWhite: '#e5e5e5'
        }
    });

    fitAddon = new FitAddon.FitAddon();
    terminal.loadAddon(fitAddon);

    const webLinksAddon = new WebLinksAddon.WebLinksAddon();
    terminal.loadAddon(webLinksAddon);

    const container = document.getElementById('terminal-container');
    container.innerHTML = '';
    terminal.open(container);
    // Do not use fitAddon to fit terminal - keep fixed size
    // fitAddon.fit();

    // Window resize events are ignored - terminal is fixed at 140x40
}

// Load available forks
async function loadForks() {
    try {
        const response = await fetch('/api/forks');
        const forks = await response.json();
        
        const select = document.getElementById('fork-select');
        select.innerHTML = '<option value="">Select a fork...</option>';
        
        forks.forEach(fork => {
            const option = document.createElement('option');
            option.value = fork.id;
            option.textContent = `${fork.id} (${fork.status})`;
            select.appendChild(option);
        });
    } catch (error) {
        console.error('Failed to load forks:', error);
        showMessage('Failed to load forks');
    }
}

// Connect to selected fork
function connectToFork() {
    const select = document.getElementById('fork-select');
    const forkId = select.value;
    
    if (!forkId) {
        alert('Please select a fork');
        return;
    }

    currentFork = forkId;
    initTerminal();
    
    // Create WebSocket connection
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/terminal/${forkId}`;
    
    socket = new WebSocket(wsUrl);
    
    socket.onopen = () => {
        terminal.writeln(`Connected to fork: ${forkId}`);
        terminal.writeln('');
        
        // Send initial resize
        socket.send(JSON.stringify({
            type: 'resize',
            rows: 40,
            cols: 140
        }));
    };
    
    socket.onmessage = async (event) => {
        if (event.data instanceof Blob) {
            // Handle binary message
            const text = await event.data.text();
            terminal.write(text);
        } else {
            // Handle text message (for backwards compatibility)
            terminal.write(event.data);
        }
    };
    
    socket.onerror = (error) => {
        terminal.writeln(`\r\nConnection error: ${error.message || 'Unknown error'}`);
    };
    
    socket.onclose = () => {
        terminal.writeln('\r\nConnection closed');
        socket = null;
    };
    
    // Handle terminal input
    terminal.onData((data) => {
        if (socket && socket.readyState === WebSocket.OPEN) {
            // Check for special keyboard shortcuts
            if (data === 's' || data === 'S') {
                // Open in VSCode
                socket.send(JSON.stringify({
                    type: 'command',
                    command: 'vscode'
                }));
            } else if (data === 'i' || data === 'I') {
                // Show container info
                socket.send(JSON.stringify({
                    type: 'command',
                    command: 'info'
                }));
            } else if (data === 'h' || data === 'H') {
                // Show help
                socket.send(JSON.stringify({
                    type: 'command',
                    command: 'help'
                }));
            } else {
                // Regular terminal input
                socket.send(data);
            }
        }
    });
    
    // Show keyboard shortcuts hint
    showShortcutsHint();
}

// Show keyboard shortcuts hint
function showShortcutsHint() {
    // Check if hint bar already exists
    if (!document.getElementById('shortcuts-hint')) {
        const hintBar = document.createElement('div');
        hintBar.id = 'shortcuts-hint';
        hintBar.innerHTML = `
            <span class="shortcut"><kbd>s</kbd> Open in VSCode</span>
            <span class="shortcut"><kbd>i</kbd> Container Info</span>
            <span class="shortcut"><kbd>h</kbd> Help</span>
        `;
        document.body.appendChild(hintBar);
    }
}

// Show message in terminal container
function showMessage(text) {
    const container = document.getElementById('terminal-container');
    container.innerHTML = `<div class="status-message">${text}</div>`;
}

// Initialize on page load
document.addEventListener('DOMContentLoaded', () => {
    loadForks();
    
    document.getElementById('connect-btn').addEventListener('click', connectToFork);
    
    // Allow Enter key to connect when fork is selected
    document.getElementById('fork-select').addEventListener('keypress', (e) => {
        if (e.key === 'Enter' && e.target.value) {
            connectToFork();
        }
    });
    
    showMessage('Select a fork to connect');
});