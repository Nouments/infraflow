#!/usr/bin/env python3
"""Append a GNS3 node's telnet console output to a file until killed."""
import sys, time, gns3lab as g
pid = g.project_id(sys.argv[1]); port = g.node(pid, sys.argv[2])["console"]
with open(sys.argv[3], "ab", buffering=0) as out:
    while True:
        try:
            con = g.Console(port)
            while True:
                data = con._read(1.0)
                if data:
                    out.write(data)
        except (OSError, ConnectionError):
            time.sleep(2)
