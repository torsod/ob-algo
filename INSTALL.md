# Installing Go and Running the Auction Book

## Install Go

### macOS

Using Homebrew:

```bash
brew install go
```

Or download the installer from https://go.dev/dl/ — choose the `.pkg` file for your architecture (Apple Silicon or Intel), open it, and follow the prompts.

Verify the installation:

```bash
go version
```

### Windows

Download the `.msi` installer from https://go.dev/dl/ and run it. The default install location is `C:\Program Files\Go`.

The installer adds `C:\Program Files\Go\bin` to your `PATH` automatically. Open a new Command Prompt or PowerShell window and verify:

```powershell
go version
```

## Build and Run

Clone or download this repository, then from the project root:

### Build

```bash
go build -o ob-algo
```

On Windows this produces `ob-algo.exe`:

```powershell
go build -o ob-algo.exe
```

### Run

macOS / Linux:

```bash
./ob-algo
```

Windows:

```powershell
.\ob-algo.exe
```

### Run without building

```bash
go run main.go
```
