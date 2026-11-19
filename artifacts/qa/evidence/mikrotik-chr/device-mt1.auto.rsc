/system identity set name="mt1"
{
	:local addressEntry [/ip address find where comment="infraflow:mt1:interface:ether1"];
	:if ([:len $addressEntry] = 0) do={/ip address add address=192.0.2.21/24 interface="ether1" comment="infraflow:mt1:interface:ether1"} else={/ip address set $addressEntry address=192.0.2.21/24 interface="ether1"}
}
{
	:local addressEntry [/ip address find where comment="infraflow:mt1:interface:ether2"];
	:if ([:len $addressEntry] = 0) do={/ip address add address=198.51.100.129/25 interface="ether2" comment="infraflow:mt1:interface:ether2"} else={/ip address set $addressEntry address=198.51.100.129/25 interface="ether2"}
}
{
	:local routeEntry [/ip route find where comment="infraflow:mt1:route:203.0.113.0/24"];
	:if ([:len $routeEntry] = 0) do={/ip route add dst-address=203.0.113.0/24 gateway="192.0.2.2" distance=1 comment="infraflow:mt1:route:203.0.113.0/24"} else={/ip route set $routeEntry dst-address=203.0.113.0/24 gateway="192.0.2.2" distance=1}
}
{
	:local ospfInstance [/routing ospf instance find where name="infraflow-v2"];
	:if ([:len $ospfInstance] = 0) do={/routing ospf instance add name=infraflow-v2 version=2 router-id=192.0.2.21} else={/routing ospf instance set $ospfInstance router-id=192.0.2.21}
}
{
	:local ospfArea [/routing ospf area find where name="area-0-0-0-0"];
	:if ([:len $ospfArea] = 0) do={/routing ospf area add name=area-0-0-0-0 area-id=0.0.0.0 instance=infraflow-v2} else={/routing ospf area set $ospfArea area-id=0.0.0.0 instance=infraflow-v2}
}
{
	:local ospfTemplate [/routing ospf interface-template find where comment="infraflow:mt1:ospf:192.0.2.0/24"];
	:if ([:len $ospfTemplate] = 0) do={/routing ospf interface-template add networks=192.0.2.0/24 area=area-0-0-0-0 comment="infraflow:mt1:ospf:192.0.2.0/24"} else={/routing ospf interface-template set $ospfTemplate networks=192.0.2.0/24 area=area-0-0-0-0}
}
{
	:local ospfTemplate [/routing ospf interface-template find where comment="infraflow:mt1:ospf:198.51.100.128/25"];
	:if ([:len $ospfTemplate] = 0) do={/routing ospf interface-template add networks=198.51.100.128/25 area=area-0-0-0-0 comment="infraflow:mt1:ospf:198.51.100.128/25"} else={/routing ospf interface-template set $ospfTemplate networks=198.51.100.128/25 area=area-0-0-0-0}
}

