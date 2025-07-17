let terminal = null;
let fitAddon = null;
let socket = null;
let currentFork = null;

// Initialize terminal
function initTerminal() {
    terminal = new Terminal({
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
    fitAddon.fit();

    // Handle window resize
    window.addEventListener('resize', () => {
        fitAddon.fit();
        if (socket && socket.readyState === WebSocket.OPEN) {
            socket.send(JSON.stringify({
                type: 'resize',
                rows: terminal.rows,
                cols: terminal.cols
            }));
        }
    });
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
        // socket.send(JSON.stringify({
        //     type: 'resize',
        //     rows: terminal.rows,
        //     cols: terminal.cols
        // }));
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
            socket.send(data);
        }
    });
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