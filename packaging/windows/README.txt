Sentrix agent for Windows
=========================

Run these in an elevated PowerShell (Run as Administrator).

1. Put sentrix-agent.exe somewhere permanent, e.g.
       C:\Program Files\Sentrix\sentrix-agent.exe

2. Enroll (the token comes from your Sentrix dashboard, "Add a host"):
       & "C:\Program Files\Sentrix\sentrix-agent.exe" enroll `
           --server https://YOUR-SERVER:8443 --token TOKEN

3. Install and start the service:
       & "C:\Program Files\Sentrix\sentrix-agent.exe" install --eventlog Application,System

   The service runs as the virtual account NT SERVICE\sentrix-agent, not
   LocalSystem. Its state directory, C:\ProgramData\sentrix-agent, is
   restricted to SYSTEM, Administrators and that account. On its first start
   the service encrypts the credential with DPAPI under its own account, so a
   copy of the directory is useless on another machine or account.

   Reading the Security log needs the account in "Event Log Readers":
       net localgroup "Event Log Readers" "NT SERVICE\sentrix-agent" /add

Logs: C:\ProgramData\sentrix-agent\agent.log
Remove: sentrix-agent.exe uninstall   (the state directory is kept)
