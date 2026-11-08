# 01 — Fondamentaux réseau

## 1. Modèles de communication

### 1.1 Modèle OSI

| Couche | Nom | Responsabilité | Exemples |
|---:|---|---|---|
| 7 | Application | Protocoles applicatifs | HTTP, DNS, SSH, SMTP |
| 6 | Présentation | Représentation, encodage, chiffrement | TLS, formats de données |
| 5 | Session | Gestion logique des échanges | Sessions RPC |
| 4 | Transport | Ports, multiplexage, fiabilité | TCP, UDP |
| 3 | Réseau | Adressage logique et routage | IPv4, IPv6, ICMP |
| 2 | Liaison | Trames, MAC, VLAN | Ethernet, 802.1Q, STP |
| 1 | Physique | Transmission des bits | Cuivre, fibre, radio |

Le modèle OSI est un outil de diagnostic. Une panne peut traverser plusieurs couches : vérifier le lien physique avant de diagnostiquer le routage.

### 1.2 TCP/IP

| Couche TCP/IP | Fonctions | Protocoles |
|---|---|---|
| Application | Services réseau | HTTP(S), DNS, SSH, DHCP, NTP |
| Transport | Ports et échanges de bout en bout | TCP, UDP |
| Internet | Adressage et routage | IPv4, IPv6, ICMP |
| Accès réseau | Trames et support | Ethernet, Wi-Fi |

### 1.3 TCP et UDP

**TCP** établit une connexion, numérote les octets, retransmet les données perdues et contrôle le flux. L'établissement classique utilise SYN, SYN-ACK puis ACK.

**UDP** transporte des datagrammes sans établir de connexion et sans garantie native de livraison ou d'ordre. L'application doit gérer elle-même les besoins de fiabilité.

Ports courants :
| Service | Port | Transport habituel |
|---|---:|---|
| SSH | 22 | TCP |
| DNS | 53 | UDP et TCP |
| DHCP serveur/client | 67/68 | UDP |
| HTTP | 80 | TCP |
| NTP | 123 | UDP |
| HTTPS | 443 | TCP ; QUIC utilise UDP/443 |
| SNMP | 161/162 | UDP |

## 2. Ethernet, MAC et ARP

Une trame Ethernet transporte notamment les adresses MAC source et destination et un champ EtherType. Les commutateurs apprennent les adresses MAC sources sur leurs ports et construisent une table de commutation.

ARP associe une adresse IPv4 à une adresse MAC sur un segment local. IPv6 utilise Neighbor Discovery (NDP), pas ARP.

À vérifier en cas de panne L2 :
- état physique du port ;
- vitesse et duplex ;
- erreurs et drops ;
- VLAN d'accès ou VLAN natif ;
- table MAC ;
- ARP/NDP ;
- MTU.

## 3. IPv4 et subnetting

Une adresse IPv4 fait 32 bits. Le préfixe CIDR indique le nombre de bits réseau.

| Préfixe | Masque | Adresses totales | Hôtes usuels |
|---:|---|---:|---:|
| /24 | 255.255.255.0 | 256 | 254 |
| /25 | 255.255.255.128 | 128 | 126 |
| /26 | 255.255.255.192 | 64 | 62 |
| /27 | 255.255.255.224 | 32 | 30 |
| /28 | 255.255.255.240 | 16 | 14 |
| /29 | 255.255.255.248 | 8 | 6 |
| /30 | 255.255.255.252 | 4 | 2 |
| /31 | 255.255.255.254 | 2 | liens point-à-point selon prise en charge |
| /32 | 255.255.255.255 | 1 | route hôte |

Pour un sous-réseau IPv4 classique :
- adresse réseau : bits hôte à zéro ;
- broadcast : bits hôte à un ;
- adresses hôtes : entre les deux.

Exceptions et usages particuliers existent, notamment /31 sur liens point-à-point et /32 pour routes hôtes.

### 3.1 Plages privées RFC 1918

| Plage | Taille |
|---|---|
| 10.0.0.0/8 | Grande plage privée |
| 172.16.0.0/12 | Plage privée intermédiaire |
| 192.168.0.0/16 | Plage privée |

### 3.2 Réseaux d'exemple

```text
LAN :       192.0.2.0/24
Passerelle: 192.0.2.1
Serveur:    192.0.2.10
Client:     192.0.2.100
WAN doc.:   203.0.113.0/24
```

Ces plages sont réservées à la documentation et ne doivent pas être utilisées comme adressage public réel.

## 4. IPv6

IPv6 utilise 128 bits. Exemple documentaire : `2001:db8:10::1/64`.

Notions essentielles :
- `fe80::/10` : adresses link-local ;
- `2000::/3` : espace global unicast actuellement attribué ;
- `ff00::/8` : multicast ;
- pas de broadcast IPv6 ;
- Neighbor Discovery remplace les fonctions ARP ;
- SLAAC et DHCPv6 peuvent participer à l'attribution des paramètres ;
- un `/64` est courant pour un LAN, mais le plan d'adressage doit suivre l'architecture.

Ne pas transposer mécaniquement les règles IPv4 NAT/PAT à IPv6. Le filtrage IPv6 reste indispensable.

## 5. VLAN et trunks

Un VLAN crée un domaine de broadcast L2 distinct. La communication entre VLAN nécessite un équipement L3 : routeur, commutateur L3 ou pare-feu.

Exemple de plan :
| VLAN | Nom | Sous-réseau documentaire |
|---:|---|---|
| 10 | MANAGEMENT | 192.0.2.0/24 |
| 20 | USERS | 198.51.100.0/24 |
| 30 | SERVERS | 203.0.113.0/24 |
| 99 | NATIVE_UNUSED | Aucun hôte |

- **Access** : porte généralement un seul VLAN non taggé.
- **Trunk 802.1Q** : transporte plusieurs VLAN taggés ; le traitement du VLAN natif dépend du constructeur.
- **SVI** : interface logique L3 associée à un VLAN.
- **Router-on-a-stick** : sous-interfaces routeur utilisant 802.1Q.

Ne pas utiliser un VLAN natif comme VLAN utilisateur par défaut. Restreindre la liste des VLAN autorisés sur les trunks.

## 6. Routage

Un routeur choisit une route selon la correspondance de préfixe la plus longue (longest-prefix match). En cas de routes de même longueur, la sélection dépend notamment de la préférence administrative et des métriques du système.

### 6.1 Route statique

Une route statique associe un préfixe destination à un next-hop ou une interface de sortie. Une route par défaut IPv4 est `0.0.0.0/0`; IPv6 `::/0`.

### 6.2 Routage dynamique

| Protocole | Famille | Usage |
|---|---|---|
| RIP | Distance-vector | Réseaux simples/anciens |
| OSPF | Link-state IGP | Routage interne |
| IS-IS | Link-state IGP | Réseaux opérateurs et grands réseaux |
| BGP | Path-vector | Interconnexion entre systèmes autonomes et politiques de routage |

### 6.3 OSPF

Concepts :
- Router ID unique dans le domaine de routage ;
- Area 0, backbone ;
- voisinage et adjacence ;
- Hello/Dead timers ;
- DR/BDR sur certains réseaux multi-accès ;
- coût d'interface ;
- annonces LSA et base LSDB.

Une adjacence ne se forme pas si des paramètres essentiels sont incompatibles : area, timers, authentification, MTU selon plateforme, type de réseau ou paramètres de sous-réseau.

### 6.4 BGP

Concepts :
- AS local et AS distant ;
- eBGP entre AS différents, iBGP dans un même AS ;
- voisinage TCP/179 ;
- préfixes annoncés et reçus ;
- attributs et politiques d'import/export ;
- filtrage strict des routes.

Ne jamais annoncer `network`/préfixe sans vérifier qu'il existe dans la table de routage selon les règles de la plateforme.

## 7. NAT

| Type | Fonction |
|---|---|
| Static NAT | Association stable entre adresses privées et publiques |
| Dynamic NAT | Traduction depuis un pool |
| PAT / overload | Plusieurs flux/adresses partagent une adresse traduite avec ports |
| DNAT | Modification de la destination, souvent pour publier un service |
| SNAT | Modification de l'adresse source |

Le NAT ne remplace pas un pare-feu. La politique de filtrage doit être conçue séparément.

## 8. DHCP

DHCPv4 utilise classiquement DORA :
1. Discover ;
2. Offer ;
3. Request ;
4. Acknowledge.

Options courantes :
| Option | Signification |
|---:|---|
| 1 | Masque IPv4 |
| 3 | Routeur/passerelle |
| 6 | Serveurs DNS |
| 15 | Nom de domaine |
| 51 | Durée du bail |
| 54 | Identifiant serveur |
| 66 | Nom du serveur TFTP |
| 67 | Nom du fichier de démarrage |
| 150 | Option propriétaire/constructeur utilisée notamment dans certains environnements Cisco pour des serveurs TFTP |

Les options 66, 67 et 150 ne constituent pas une méthode ZTP universelle. Leur rôle dépend du client DHCP et du mécanisme de provisioning de la plateforme.

Un DHCP relay transmet les requêtes entre un réseau client et un serveur DHCP distant. Le relay renseigne des informations de relais utilisées par le serveur pour choisir le scope.

## 9. DNS, NTP et journalisation

- DNS A : nom vers IPv4.
- DNS AAAA : nom vers IPv6.
- DNS PTR : résolution inverse.
- CNAME : alias.
- MX : serveur de messagerie.
- TXT : données textuelles et politiques.
- SRV : découverte de services.

NTP synchronise l'horloge. Une horloge correcte est indispensable pour corréler les logs, certificats et événements de sécurité.

Syslog transporte les événements vers un collecteur central. Définir le niveau de sévérité, le transport, la rétention et la protection du canal.

## 10. Pare-feu et ACL

Une ACL filtre des paquets selon des critères tels que source, destination, protocole et ports. Un pare-feu stateful maintient un état de session.

États fréquents :
- NEW ;
- ESTABLISHED ;
- RELATED ;
- INVALID.

Méthode :
1. autoriser explicitement les flux nécessaires ;
2. limiter les accès d'administration aux réseaux autorisés ;
3. journaliser les événements utiles ;
4. appliquer un refus par défaut lorsque cela correspond à la politique ;
5. tester les flux autorisés et interdits.

Une règle `permit ip any any` ou `service ALL` peut ouvrir beaucoup plus que prévu. Les exemples permissifs doivent rester isolés en laboratoire.

## 11. QoS

| Mécanisme | Rôle |
|---|---|
| Classification | Identifier les flux |
| Marking | Marquer DSCP/CoS |
| Queuing | Organiser les files |
| Scheduling | Déterminer le service des files |
| Shaping | Lisser le débit |
| Policing | Mesurer et limiter, éventuellement jeter ou remarquer |

La QoS ne crée pas de bande passante. Elle arbitre la congestion. La politique doit être cohérente de bout en bout.

## 12. Haute disponibilité

- STP/RSTP/MSTP : prévention des boucles L2 ;
- LACP : agrégation de liens ;
- HSRP : redondance de passerelle Cisco ;
- VRRP : redondance de passerelle multi-constructeurs ;
- HA firewall : actif/passif ou actif/actif selon plateforme.

Une redondance doit être testée : perte de lien, perte d'équipement, convergence, synchronisation d'état et retour au nominal.
