using System;
using System.Collections.Generic;
using System.IO;
using System.Net;
using System.Threading;
using DiskAccessLibrary;
using ISCSI.Server;
using ISCSI.Logging;

namespace NKBDiskless.Daemon
{
    class Program
    {
        static string logFile = "iscsi_server.log";

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
            int port = 3260;

            for (int i = 0; i < args.Length; i++)
            {
                if (args[i] == "-target" && i + 1 < args.Length) targetName = args[++i];
                if (args[i] == "-vhd" && i + 1 < args.Length) vhdPath = args[++i];
                if (args[i] == "-port" && i + 1 < args.Length) int.TryParse(args[++i], out port);
            }

            Log(" Target IQN : " + targetName);
            Log(" VHD Image  : " + vhdPath);
            Log(" Port       : " + port);
            Log("=================================================================");

            if (!File.Exists(vhdPath))
            {
                Log("[X] FATAL: VHD file not found: " + vhdPath);
                return;
            }

            Disk disk = null;
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

            List<Disk> diskList = new List<Disk>();
            diskList.Add(disk);

            ISCSI.Server.ISCSITarget target = new ISCSI.Server.ISCSITarget(targetName, diskList);
            ISCSIServer server = new ISCSIServer();
            server.AddTarget(target);

            server.OnLogEntry += (sender, log) =>
            {
                Log(string.Format("[ISCSI-{0}] {1}", log.Severity, log.Message));
            };

            IPEndPoint ep = new IPEndPoint(IPAddress.Any, port);
            try
            {
                server.Start(ep);
                Log("[✓] iSCSI Target Server is ACTIVE on 0.0.0.0:" + port);
                Log(">>> READY for Dell Client (28:00:AF:A8:44:CC) to boot via SANBOOT.");
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
