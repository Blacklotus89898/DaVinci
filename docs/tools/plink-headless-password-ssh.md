# plink headless password ssh

## Headless password SSH from Windows via plink (-batch -hostkey -m file)

### Purpose
Run non-interactive SSH commands against a host where we only have **password** auth (no sshpass on Windows; OpenSSH can't automate passwords; and plink's interactive host-key/password prompts block forever under headless runners — they read the console, not piped stdin).

### Script
```powershell
# 1. Pin the host key (get fingerprint from OpenSSH known_hosts after one accept-new connect)
$line = ssh-keygen -F proxmox.home -f "$env:USERPROFILE\.ssh\known_hosts" | Where-Object { $_ -notmatch '^#' } | Select-Object -First 1
Set-Content $env:TEMP\hostkey $line
ssh-keygen -lf $env:TEMP\hostkey          # → "256 SHA256:XXXX... host (ED25519)"

# 2. Put the remote script in a FILE (never as an argv string — PS 5.1 shreds multiline argv)
[IO.File]::WriteAllText($env:TEMP\remote.sh, $scriptHereString.Replace("`r`n","`n"))

# 3. Run fully non-interactive
plink -ssh -batch -hostkey "SHA256:XXXX..." -pw '<password>' -m $env:TEMP\remote.sh root@proxmox.home
```

### Gotchas
- `echo y | plink ...` **does not work headless**: plink's first-connection host-key prompt reads the console and hangs forever with zero output (symptom: command times out, output file empty).
- `-batch` + `-hostkey <fingerprint>` is the deterministic headless combination.
- `-m file` beats passing the command as an argument for anything multiline.
- Password on the CLI is visible in process lists briefly — fine on a personal machine; never write it to files, KB, or commits.
