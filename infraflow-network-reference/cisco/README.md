# Cisco IOS / IOS XE — Référence CLI

**Statut :** exemples pédagogiques ; syntaxe et disponibilité à vérifier selon modèle et release.

## 1. Modes CLI

| Mode | Invite typique | Usage |
|---|---|---|
| User EXEC | `Router>` | Consultation limitée |
| Privileged EXEC | `Router#` | Vérification et administration |
| Global config | `Router(config)#` | Configuration globale |
| Interface config | `Router(config-if)#` | Interface |
| Line config | `Router(config-line)#` | Console/VTY |

```text
enable
configure terminal
hostname INFRA-RTR-01
end
show running-config
copy running-config startup-config
```

`write memory` est également présent sur de nombreuses plateformes, mais les commandes de sauvegarde doivent être validées pour la cible.

## 2. Identité et accès

Exemple de compte local :
```text
username infraadmin privilege 15 secret <SECRET>
enable secret <ENABLE_SECRET>
```

SSH :
```text
ip domain-name lab.invalid
crypto key generate rsa modulus 2048
ip ssh version 2
line vty 0 4
 login local
 transport input ssh
```

Les options exactes de génération de clés varient selon la release. Restreindre les VTY par ACL de management. Ne pas activer Telnet en production.

## 3. Interfaces L3

```text
interface GigabitEthernet0/0/0
 description WAN-UPLINK
 ip address dhcp
 no shutdown
!
interface GigabitEthernet0/0/1
 description LAN
 ip address 192.0.2.1 255.255.255.0
 no shutdown
```

Les noms d'interfaces sont dépendants du modèle. Les commutateurs L2 peuvent ne pas accepter `ip address` sur un port physique en mode switchport.

## 4. SVI et VLAN

```text
vlan 10
 name MANAGEMENT
vlan 20
 name USERS
vlan 30
 name SERVERS
!
interface Vlan10
 description MANAGEMENT-GATEWAY
 ip address 192.0.2.1 255.255.255.0
 no shutdown
```

Trunk :
```text
interface GigabitEthernet1/0/48
 description UPLINK
 switchport mode trunk
 switchport trunk allowed vlan 10,20,30
```

Port access :
```text
interface GigabitEthernet1/0/10
 description USER-PORT
 switchport mode access
 switchport access vlan 20
 spanning-tree portfast
```

`spanning-tree portfast` ne doit être utilisé que sur les ports edge destinés aux terminaux, pas sur un lien entre commutateurs.

## 5. DHCP

Serveur DHCP (sur les plateformes qui supportent cette fonction) :
```text
ip dhcp excluded-address 192.0.2.1 192.0.2.20
ip dhcp pool USERS
 network 192.0.2.0 255.255.255.0
 default-router 192.0.2.1
 dns-server 192.0.2.53
 domain-name lab.invalid
```

Relay :
```text
interface Vlan20
 ip helper-address 198.51.100.10
```

Client :
```text
interface GigabitEthernet0/0/0
 ip address dhcp
```

Le relay et le serveur doivent être configurés selon le plan de routage et les scopes DHCP.

## 6. Routage statique

```text
ip route 198.51.100.0 255.255.255.0 192.0.2.254
ip route 0.0.0.0 0.0.0.0 192.0.2.254
```

Vérification :
```text
show ip route
show ip interface brief
```

## 7. OSPFv2

```text
router ospf 10
 router-id 192.0.2.1
 passive-interface default
 no passive-interface GigabitEthernet0/0/1
 network 192.0.2.0 0.0.0.255 area 0
```

Vérification :
```text
show ip ospf neighbor
show ip ospf interface brief
show ip route ospf
show ip protocols
```

L'interface autorisée à former une adjacence doit être choisie selon la topologie. Vérifier area, timers, authentification, MTU et type de réseau.

## 8. BGP IPv4

Exemple pédagogique :
```text
router bgp 65001
 bgp router-id 192.0.2.1
 neighbor 203.0.113.1 remote-as 65002
 address-family ipv4
  network 192.0.2.0 mask 255.255.255.0
  neighbor 203.0.113.1 activate
 exit-address-family
```

Les politiques d'import/export, prefix-lists, route-maps, max-prefix et protections de session sont indispensables dans un vrai déploiement. La syntaxe varie selon IOS/IOS XE.

## 9. NAT/PAT

Exemple PAT :
```text
access-list 10 permit 192.0.2.0 0.0.0.255
interface GigabitEthernet0/0/0
 ip nat outside
interface GigabitEthernet0/0/1
 ip nat inside
ip nat inside source list 10 interface GigabitEthernet0/0/0 overload
```

Vérification :
```text
show ip nat translations
show ip nat statistics
```

L'ACL utilisée pour sélectionner les sources NAT n'est pas automatiquement une politique de sécurité complète.

## 10. ACL IPv4

```text
ip access-list extended USERS-OUT
 permit tcp 192.0.2.0 0.0.0.255 any eq 443
 permit udp 192.0.2.0 0.0.0.255 any eq 53
 deny ip any any log
!
interface Vlan20
 ip access-group USERS-OUT in
```

Les ACL sont évaluées dans l'ordre ; une règle implicite de refus existe à la fin. Vérifier le sens d'application et l'impact sur DHCP, DNS, routage et administration.

## 11. QoS — squelette conceptuel

```text
class-map match-any VOICE
 match dscp ef
!
policy-map WAN-OUT
 class VOICE
  priority percent 20
 class class-default
  fair-queue
!
interface GigabitEthernet0/0/0
 service-policy output WAN-OUT
```

Les commandes QoS et leur disponibilité dépendent fortement de la plateforme et de la capacité matérielle. Dimensionner la file prioritaire et éviter une réservation excessive.

## 12. NTP, syslog et SNMP

```text
ntp server 192.0.2.123
logging host 192.0.2.60
logging trap informational
```

SNMPv3 doit être préféré. Exemple conceptuel à adapter :
```text
snmp-server group MONITOR v3 priv
snmp-server user monitor MONITOR v3 auth sha <AUTH_SECRET> priv aes 128 <PRIV_SECRET>
```

La syntaxe exacte dépend de la release. Ne pas utiliser une community publique/commune en production.

## 13. Sauvegarde et vérification

```text
show running-config
show startup-config
copy running-config startup-config
show version
show inventory
show interfaces status
show ip route
show logging
```

Avant une modification distante, conserver une copie de la configuration et prévoir un accès de secours.

## 14. Dépannage rapide

| Symptôme | Contrôles |
|---|---|
| Interface down | `show interfaces`, câble, module, shutdown |
| Pas d'adresse DHCP | VLAN, scope, relay, serveur, ACL |
| Pas de ping inter-VLAN | SVI, `ip routing`, routes, ACL |
| OSPF sans voisin | area, timers, MTU, réseau, passive-interface |
| BGP Idle/Active | reachability, TCP/179, AS, source, policy |
| NAT vide | inside/outside, ACL NAT, route, trafic |
| Trunk incorrect | mode, VLAN autorisés, native VLAN |

## 15. AutoInstall et ZTP — avertissement

Cisco AutoInstall et Cisco ZTP sont des mécanismes différents. Leur disponibilité et leur séquence dépendent du modèle, de la famille logicielle et de la version.

Ne pas conclure qu'un routeur démarre nécessairement un script Python simplement parce qu'il n'a pas de startup-config. Ne pas supposer que DHCP option 67 et option 150 suffisent pour toutes les plateformes. Avant de générer un workflow :
1. identifier exactement le modèle et la version ;
2. consulter le guide officiel correspondant ;
3. confirmer le protocole de découverte, le format de bootstrap et les options DHCP réellement attendues ;
4. confirmer l'environnement d'exécution du script, s'il existe ;
5. tester sur la plateforme concernée.

Cette référence ne fournit pas une procédure universelle ZTP/AutoInstall.
