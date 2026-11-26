using System;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Threading;
using DiskAccessLibrary;
using DiskAccessLibrary.Win32;
using ISCSI.Server;
using ISCSI.Logging;

namespace NKBDiskless.Daemon
{
    class Program
    {
        static string logFile = @"C:\NKB-PoC\iscsi_server.log";

        static void Log(string msg)
        {
            string line = string.Format("[{0:yyyy-MM-dd HH:mm:ss}] {1}", DateTime.Now, msg);
            Console.WriteLine(msg);
            try { File.AppendAllText(logFile, line + Environment.NewLine); } catch { }
        }

        static void Main(string[] args)
        {
            Log("=================================================================");
            Log("        NKB DISKLESS SYSTEM - iSCSI BLOCK STORAGE DAEMON        ");
            Log("=================================================================");

            string targetName = "iqn.2026-11.com.nkb:client-2800afa844cc";
            string vhdPath = @"C:\NKB-PoC\disks\virtual\prod_2.vhd";
            int diskNumber = -1;
            int port = 3260;
            long sizeGb = 100;

            for (int i = 0; i < args.Length; i++)
            {
                if (args[i] == "-target" && i + 1 < args.Length) targetName = args[++i];
                if (args[i] == "-vhd" && i + 1 < args.Length) vhdPath = args[++i];
                if (args[i] == "-disk" && i + 1 < args.Length) int.TryParse(args[++i], out diskNumber);
                if (args[i] == "-port" && i + 1 < args.Length) int.TryParse(args[++i], out port);
                if (args[i] == "-size" && i + 1 < args.Length) long.TryParse(args[++i], out sizeGb);
            }

            Log(" Target IQN  : " + targetName);
            Log(" VHD Path    : " + vhdPath);
            Log(" Disk Number : " + (diskNumber >= 0 ? diskNumber.ToString() : "N/A"));
            Log(" Port        : " + port);
            Log(" Target Size : " + sizeGb + " GB");
            Log("=================================================================");

            Disk disk = null;

            if (diskNumber >= 0)
            {
                try
                {
                    Log(string.Format("[*] Opening PhysicalDisk {0} (Read/Write)...", diskNumber));
                    disk = new PhysicalDisk(diskNumber, false);
                    Log(string.Format("[✓] PhysicalDisk {0} opened. Size: {1} GB", diskNumber, disk.Size / (1024 * 1024 * 1024)));
                }
                catch (Exception ex)
                {
                    Log(string.Format("[!] PhysicalDisk {0} failed: {1}. Falling back to VHD...", diskNumber, ex.Message));
                    disk = null;
                }
            }

            if (disk == null)
            {
                // If VHD exists but is fresh/empty (< 10MB on disk), recreate it with desired size
                if (File.Exists(vhdPath))
                {
                    try
                    {
                        FileInfo fi = new FileInfo(vhdPath);
                        if (fi.Length < 10 * 1024 * 1024)
                        {
                            Log(string.Format("[*] Refreshing blank dynamic VHD to requested {0} GB...", sizeGb));
                            File.Delete(vhdPath);
                        }
                    }
                    catch (Exception exDel)
                    {
                        Log("[!] Could not delete old VHD: " + exDel.Message);
                    }
                }

                if (!File.Exists(vhdPath))
                {
                    try
                    {
                        string dir = Path.GetDirectoryName(vhdPath);
                        if (!string.IsNullOrEmpty(dir) && !Directory.Exists(dir))
                        {
                            Directory.CreateDirectory(dir);
                        }
                        Log(string.Format("[*] Creating {0} GB Dynamic VHD: {1}", sizeGb, vhdPath));
                        long sizeBytes = sizeGb * 1024L * 1024L * 1024L;
                        VirtualHardDisk.CreateDynamicDisk(vhdPath, sizeBytes);
                        Log(string.Format("[✓] {0} GB Dynamic VHD successfully created.", sizeGb));
                    }
                    catch (Exception exCreate)
                    {
                        Log("[X] Failed to create VHD: " + exCreate.Message);
                    }
                }

                if (!File.Exists(vhdPath))
                {
                    Log("[X] FATAL: VHD file not found: " + vhdPath);
                    return;
                }

                try
                {
                    Log("[*] Opening Virtual Hard Disk (Read/Write)...");
                    disk = new VirtualHardDisk(vhdPath, false);
                    Log("[✓] VHD opened (Read/Write). Size: " + (disk.Size / (1024 * 1024 * 1024)) + " GB");
                }
                catch (Exception ex)
                {
                    Log("[!] Read/Write failed: " + ex.Message + ". Attempting Read-Only...");
                    try
                    {
                        disk = new VirtualHardDisk(vhdPath, true);
                        Log("[✓] VHD opened (Read-Only). Size: " + (disk.Size / (1024 * 1024 * 1024)) + " GB");
                    }
                    catch (Exception ex2)
                    {
                        Log("[X] FATAL: Could not open VHD: " + ex2.ToString());
                        return;
                    }
                }
            }

            List<Disk> diskList = new List<Disk>();
            diskList.Add(disk);

            ISCSIServer server = new ISCSIServer();
            server.AddTarget(new ISCSI.Server.ISCSITarget(targetName, diskList));

            // Also register common aliases so any client connection succeeds
            string[] commonAliases = new string[] {
                "iqn.2026-11.com.nkb:client-2800afa844cc",
                "iqn.2026-11.com.nkb:client-0492264bd5eb",
                "iqn.2026-11.com.nkb:default",
                "iqn.2026-11.com.nkb:master"
            };

            foreach (string alias in commonAliases)
            {
                if (!alias.Equals(targetName, StringComparison.OrdinalIgnoreCase))
                {
                    try { server.AddTarget(new ISCSI.Server.ISCSITarget(alias, diskList)); } catch { }
                }
            }

            server.OnLogEntry += (sender, log) =>
            {
                Log(string.Format("[ISCSI-{0}] {1}", log.Severity, log.Message));
            };

            IPEndPoint ep = new IPEndPoint(IPAddress.Any, port);
            try
            {
                server.Start(ep);
                Log("[✓] iSCSI Target Server is ACTIVE on 0.0.0.0:" + port);
                Log(">>> READY for Diskless Clients (e.g. 28:00:AF:A8:44:CC) to boot or install via SANBOOT / SANHOOK.");
                Log("=================================================================");
            }
            catch (Exception ex)
            {
                Log("[X] FATAL: Could not bind port " + port + ": " + ex.ToString());
                return;
            }

            while (true)
            {
                Thread.Sleep(1000);
            }
        }
    }
}
