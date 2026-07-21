# StarCraft II Multibox Closer (`sc2-multibox`)

A lightweight, robust Windows utility written in Go that closes the StarCraft II single-instance lock handles. This allows you to launch and run multiple StarCraft II clients simultaneously on the same machine (for multi-boxing, multi-account testing, etc.).

---

## How It Works

When you start StarCraft II, the game creates named kernel objects (specifically `Event` and `Section` objects) in the Windows Object Manager:
- `\Sessions\<Session ID>\BaseNamedObjects\StarCraft II Game Application` (or globally without the session prefix)
- `\Sessions\<Session ID>\BaseNamedObjects\StarCraft II IPC Mem`

If another StarCraft II client starts up and detects these objects already exist, it refuses to launch.

This tool locates all running `SC2_x64.exe` processes, safely duplicates their handle tables, identifies the lock handles using dynamic type index queries (to prevent deadlocks or thread hangs), and closes them in the game processes. Once the handles are closed, you can launch a new StarCraft II client alongside the existing one(s).

---

## Prerequisites

- **Operating System**: Windows (64-bit recommended)
- **Permissions**: **Administrator Rights** (the application will automatically prompt you for UAC elevation when run, as querying system handles requires admin privileges).

---

## Option 1: Using the Pre-built Release (easiest)

1. Go to the **Releases** tab on the GitHub repository.
2. Download the latest `sc2-multibox-windows-amd64.exe`.
3. Start your first StarCraft II client.
4. Run the downloaded `sc2-multibox-windows-amd64.exe`.
5. Agree to the Windows Administrator prompt (UAC).
6. The console window will open, list the StarCraft II process, show that the lock handles were successfully closed, and close itself after a few seconds.
7. Launch your second StarCraft II client!

---

## Option 2: Build It Yourself from Source (Highly Recommended for Security)

If you prefer not to run pre-compiled binaries from the internet, you can easily build this tool yourself in under 2 minutes. The code is completely open-source and contains no third-party libraries except the standard, official Go Windows system packages.

### Step 1: Install Go (Golang)
To compile Go code, you need the Go compiler installed on your computer:
1. Go to the official Go download page: [https://go.dev/dl/](https://go.dev/dl/)
2. Click on the installer link for Windows (usually named something like `go1.x.x.windows-amd64.msi`).
3. Run the downloaded installer and follow the prompt (just click "Next" and "Install" using default settings).
4. Once completed, restart any open command lines.

### Step 2: Verify Go is Installed
1. Press the **Windows Key**, type `cmd` (Command Prompt) or `powershell` (PowerShell), and press Enter.
2. In the terminal, type:
   ```cmd
   go version
   ```
3. Press Enter. You should see output similar to: `go version go1.26.3 windows/amd64`. If you see this, Go is ready to use!

### Step 3: Download the Source Code
1. Click the green **Code** button at the top right of this GitHub page.
2. Select **Download ZIP**.
3. Once downloaded, extract the ZIP file to any folder on your computer (e.g., your Desktop).

### Step 4: Compile the Program
1. Open Command Prompt or PowerShell.
2. Navigate to the folder where you extracted the files (the folder containing `main.go`). You can do this by typing `cd` followed by a space, and then dragging the folder into the terminal window and hitting Enter.
   *Example:*
   ```cmd
   cd C:\Users\YourUsername\Desktop\sc2-multibox
   ```
3. Run the following command to compile the program:
   ```cmd
   go build -o sc2-multibox.exe main.go
   ```
4. This will compile the code and generate a brand-new file called `sc2-multibox.exe` directly inside that folder!

### Step 5: Run Your Custom Build
1. Open StarCraft II.
2. Double-click the `sc2-multibox.exe` you just generated.
3. Accept the Administrator prompt.
4. Launch another instance of StarCraft II.

---

## Troubleshooting & FAQ

#### Why does it require Administrator privileges?
Windows restricts processes from viewing, duplicating, or modifying handles belonging to other processes. To query the system-wide handle table (via `NtQuerySystemInformation`) and duplicate handles out of `SC2_x64.exe` (via `DuplicateHandle`), the operating system requires administrative permissions.

#### My antivirus flagged this program. Is it safe?
Because this tool elevates to Administrator and manipulates the handle table of another process (`SC2_x64.exe`), some heuristic antivirus checkers might flag it. This is a common false-positive for game utility tools. Because the source code is entirely open and under 400 lines, we encourage you to inspect `main.go` and compile it yourself using **Option 2** if you have any safety concerns.

#### The program closes immediately, did it work?
Yes! The program is designed to run, close the handles, and exit. It includes a 3-second sleep timer at the end so you can inspect the output in the console window before it closes.
