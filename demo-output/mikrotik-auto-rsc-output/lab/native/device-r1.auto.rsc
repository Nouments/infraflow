/system identity set name="edge-r1"
{
	:local addressEntry [/ip address find where comment="infraflow:r1:interface:ether1"];
	:if ([:len $addressEntry] = 0) do={/ip address add address=198.51.100.1/24 interface="ether1" comment="infraflow:r1:interface:ether1"} else={/ip address set $addressEntry address=198.51.100.1/24 interface="ether1"}
}
{
	:local dhcpEntry [/ip dhcp-client find where comment="infraflow:r1:interface:ether2"];
	:if ([:len $dhcpEntry] = 0) do={/ip dhcp-client add interface="ether2" disabled=no comment="infraflow:r1:interface:ether2"} else={/ip dhcp-client set $dhcpEntry interface="ether2" disabled=no}
}
{
	:local routeEntry [/ip route find where comment="infraflow:r1:route:203.0.113.0/24"];
	:if ([:len $routeEntry] = 0) do={/ip route add dst-address=203.0.113.0/24 gateway="198.51.100.254" distance=1 comment="infraflow:r1:route:203.0.113.0/24"} else={/ip route set $routeEntry dst-address=203.0.113.0/24 gateway="198.51.100.254" distance=1}
}
