# MikroTik RouterOS 7 — Référence CLI

**Statut :** exemples pédagogiques RouterOS 7 ; vérifier la version exacte et le matériel.

## 1. Connexion et identité

```bash
ssh admin@192.0.2.1
```

```routeros
/system/identity/set name=INFRA-RTR-01
/system/resource/print
/system/routerboard/print
```

Créer un compte dédié et vérifier son accès avant de désactiver le compte initial :
```routeros
/user/add name=infraadmin group=full password="<SECRET>"
```

Les comptes `full` ont des privilèges élevés. Utiliser des groupes personnalisés lorsque cela est possible.

## 2. Services d'administration

```routeros
/ip/service/print
/ip/service/disable telnet
/ip/service/disable ftp
/ip/service/disable www
```

Restreindre les services autorisés aux réseaux de management. Ne pas désactiver le service utilisé pour la session d'administration en cours avant d'avoir validé un accès alternatif.

## 3. Interfaces et adresses

```routeros
/interface/print
/interface/set [find name="ether2"] comment="LAN-CORE"
/ip/address/add address=192.0.2.1/24 interface=ether2 comment="LAN"
```

Vérification :
```routeros
/ip/address/print
/interface/monitor-traffic ether2
```

## 4. Bridge et VLAN

Créer un bridge :
```routeros
/interface/bridge/add name=BR-LAN vlan-filtering=no
/interface/bridge/port/add bridge=BR-LAN interface=ether2
/interface/bridge/port/add bridge=BR-LAN interface=ether3
```

Exemple de table VLAN (à adapter à la topologie et aux ports tagged/untagged) :
```routeros
/interface/bridge/vlan/add bridge=BR-LAN vlan-ids=20 tagged=BR-LAN,ether2 untagged=ether3
```

Activer `vlan-filtering=yes` uniquement après avoir préparé et vérifié la table VLAN et prévu un accès de secours. Une mauvaise configuration peut couper l'administration.

Interface VLAN L3 :
```routeros
/interface/vlan/add name=VLAN20 vlan-id=20 interface=BR-LAN
/ip/address/add address=198.51.100.1/24 interface=VLAN20
```

## 5. DHCP client

```routeros
/ip/dhcp-client/add interface=ether1 add-default-route=yes use-peer-dns=no disabled=no
/ip/dhcp-client/print detail
```

Vérifier que la route par défaut et les DNS appris correspondent à la politique voulue.

## 6. DHCP server

Exemple simple :
```routeros
/ip/pool/add name=POOL-USERS ranges=198.51.100.100-198.51.100.200
/ip/dhcp-server/add name=DHCP-USERS interface=VLAN20 address-pool=POOL-USERS disabled=no
/ip/dhcp-server/network/add address=198.51.100.0/24 gateway=198.51.100.1 dns-server=192.0.2.53
```

Le réseau, l'interface, le pool et la passerelle doivent correspondre. Vérifier les leases et les logs.

## 7. Routage statique

```routeros
/ip/route/add dst-address=203.0.113.0/24 gateway=198.51.100.254
/ip/route/add dst-address=0.0.0.0/0 gateway=198.51.100.254
/ip/route/print detail
```

## 8. OSPFv2 RouterOS 7

Exemple de structure RouterOS 7 :
```routeros
/routing/ospf/instance/add name=default-v2 version=2 router-id=192.0.2.1
/routing/ospf/area/add name=backbone area-id=0.0.0.0 instance=default-v2
/routing/ospf/interface-template/add area=backbone networks=198.51.100.0/24
```

La sélection par `networks` et les propriétés de l'interface-template doivent être adaptées à la topologie et à la version. Vérifier :
```routeros
/routing/ospf/neighbor/print detail
/routing/ospf/route/print
/log/print
```

## 9. BGP RouterOS 7

Exemple minimal à adapter :
```routeros
/routing/bgp/connection/add name=UPSTREAM local.as=65001 local.role=ebgp remote.address=203.0.113.1 remote.as=65002
/routing/bgp/session/print detail
```

Définir explicitement les templates, address-families, filtres d'entrée/sortie et annonces selon la version et le scénario. Ne pas établir un peering de production sans politiques de filtrage.

## 10. NAT

Masquerade :
```routeros
/ip/firewall/nat/add chain=srcnat out-interface=ether1 action=masquerade comment="LAN to WAN"
```

Destination NAT :
```routeros
/ip/firewall/nat/add chain=dstnat in-interface=ether1 protocol=tcp dst-port=443 action=dst-nat to-addresses=203.0.113.10 to-ports=443
```

Vérifier que l'adresse interne, le routage retour et les règles firewall autorisent le flux.

## 11. Firewall filter

Exemple pédagogique de base :
```routeros
/ip/firewall/filter/add chain=input action=accept connection-state=established,related,untracked
/ip/firewall/filter/add chain=input action=drop connection-state=invalid
/ip/firewall/filter/add chain=input action=accept protocol=icmp
/ip/firewall/filter/add chain=input action=accept in-interface=ether1 protocol=tcp dst-port=22 src-address=192.0.2.0/24
/ip/firewall/filter/add chain=input action=drop in-interface=ether1
```

Attention : cet exemple ne constitue pas une politique complète. Il faut autoriser les flux de management réellement nécessaires, dans le bon ordre, et traiter le trafic `forward` séparément du trafic `input`.

## 12. QoS / queues

Exemple simple :
```routeros
/queue/simple/add name=USERS target=198.51.100.0/24 max-limit=20M/20M
```

Les queues simples, Queue Tree, PCQ et FastTrack interagissent. Vérifier les effets de FastTrack sur la QoS et le comptage de trafic.

## 13. NTP, logs et sauvegarde

```routeros
/system/ntp/client/set enabled=yes
/system/ntp/client/servers/add address=192.0.2.123
/system/logging/print
/export file=INFRA-RTR-01
/system/backup/save name=INFRA-RTR-01
```

Les exports texte et sauvegardes binaires n'ont pas le même usage. Protéger les fichiers, car ils peuvent contenir des informations sensibles.

## 14. Vérification et dépannage

```routeros
/interface/print
/ip/address/print
/ip/route/print detail
/ip/arp/print
/ip/firewall/filter/print stats
/ip/firewall/nat/print stats
/ip/firewall/connection/print
/routing/ospf/neighbor/print detail
/routing/bgp/session/print detail
/log/print
```

## 15. Provisioning et Netinstall

Les méthodes de provisioning dépendent du modèle, du RouterBOOT, de RouterOS et des mécanismes activés. Ne pas assimiler un accès API/SSH à un ZTP universel.

Netinstall et reset peuvent réinstaller ou effacer la configuration. Toute opération de ce type exige une confirmation explicite, une sauvegarde et une procédure de récupération adaptée au matériel.
