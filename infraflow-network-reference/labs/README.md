# Laboratoires multi-constructeurs

Les laboratoires utilisent uniquement des plages réservées à la documentation. Adapter les interfaces et capacités au modèle réellement utilisé.

## Lab 1 — Routeur avec WAN DHCP et LAN statique

### Objectif
- WAN : client DHCP ;
- LAN : `192.0.2.1/24` ;
- vérifier l'adresse WAN, la route par défaut et la connectivité.

### Cisco IOS / IOS XE
```text
interface GigabitEthernet0/0/0
 description WAN
 ip address dhcp
 no shutdown
!
interface GigabitEthernet0/0/1
 description LAN
 ip address 192.0.2.1 255.255.255.0
 no shutdown
```

### MikroTik RouterOS 7
```routeros
/ip/dhcp-client/add interface=ether1 add-default-route=yes use-peer-dns=no
/ip/address/add address=192.0.2.1/24 interface=ether2
```

### FortiOS
```fortios
config system interface
    edit "port1"
        set mode dhcp
    next
    edit "port2"
        set mode static
        set ip 192.0.2.1 255.255.255.0
    next
end
```

## Lab 2 — Segmentation VLAN

| VLAN | Rôle | Réseau |
|---:|---|---|
| 10 | Management | 192.0.2.0/24 |
| 20 | Utilisateurs | 198.51.100.0/24 |
| 30 | Serveurs | 203.0.113.0/24 |

Objectifs :
- créer les VLAN ;
- configurer un trunk ;
- configurer une passerelle L3 pour chaque VLAN ;
- vérifier l'isolation L2 et le routage inter-VLAN ;
- appliquer une politique limitant les flux entre VLAN.

Ne pas utiliser les mêmes commandes entre constructeurs. Consulter les chapitres dédiés.

## Lab 3 — OSPF à deux routeurs

Topologie documentaire :
```text
R1 192.0.2.1/30 ---- 192.0.2.2/30 R2
R1 LAN 198.51.100.0/24
R2 LAN 203.0.113.0/24
Area 0
```

Critères :
- Router IDs uniques ;
- adjacence OSPF établie ;
- réseaux LAN annoncés ;
- routes apprises visibles ;
- interfaces LAN passives si aucune adjacence n'est attendue ;
- test de convergence après coupure du lien.

## Lab 4 — NAT sortant

Objectif : traduire les sources du LAN vers l'adresse WAN. Vérifier la route, la politique de filtrage et les compteurs NAT. Le NAT ne remplace pas le filtrage.

## Lab 5 — Dépannage méthodique

Injecter une panne à la fois :
1. interface administrativement down ;
2. mauvais VLAN access ;
3. VLAN absent du trunk ;
4. mauvaise passerelle ;
5. route manquante ;
6. ACL bloquant DNS ;
7. voisin OSPF non formé ;
8. règle NAT incorrecte.

Pour chaque panne, noter le symptôme, les commandes d'observation, la cause et la correction. Ne pas modifier plusieurs paramètres simultanément.
