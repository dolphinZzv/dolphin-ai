using System;
using System.Diagnostics;

namespace Dolphin.WebHost
{
    internal static class Logger
    {
        public static void Info(string message)
        {
            Trace.WriteLine($"[INFO] {DateTime.UtcNow:O} {message}");
        }

        public static void Warn(string message)
        {
            Trace.WriteLine($"[WARN] {DateTime.UtcNow:O} {message}");
        }

        public static void Error(string message)
        {
            Trace.WriteLine($"[ERROR] {DateTime.UtcNow:O} {message}");
        }

        public static void Error(Exception ex, string message)
        {
            Trace.WriteLine($"[ERROR] {DateTime.UtcNow:O} {message}: {ex}");
        }
    }
}