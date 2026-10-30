# Fortinet FortiOS / FortiGate — Référence CLI

**Statut :** exemples pédagogiques ; vérifier la version FortiOS, le modèle, les VDOM et les licences.

## 1. Concepts FortiGate

- Les interfaces peuvent être physiques, VLAN, agrégées ou logiques.
- Les politiques firewall contrôlent les flux entre interfaces/zones.
- Les objets d'adresses et de services facilitent la réutilisation.
- Le NAT peut être activé dans une policy ou configuré par des mécanismes dédiés selon l'architecture.
- Les VDOM isolent des contextes logiques lorsque cette fonction est disponible.
- Les fonctions et syntaxes changent selon modèle et version.

## 2. Connexion et identité

```bash
ssh admin@192.0.2.1
```

```fortios
config system global
    set hostname INFRA-FGT-01
end
```

Créer les administrateurs selon le principe du moindre privilège. Ne pas utiliser systématiquement `super_admin` pour les comptes d'exploitation.

## 3. Interfaces

```fortios
config system interface
    edit "port1"
        set mode dhcp
        set allowaccess ping
    next
    edit "port2"
        set mode static
        set ip 192.0.2.1 255.255.255.0
        set allowaccess ping
    next
end
```

N'activer HTTPS/SSH sur une interface que si l'administration doit réellement y être accessible. Restreindre l'accès aux réseaux de management.

## 4. VLAN

```fortios
config system interface
    edit "VLAN20"
        set interface "port2"
        set vlanid 20
        set ip 198.51.100.1 255.255.255.0
        set allowaccess ping
    next
end
```

La syntaxe exacte et les restrictions dépendent de la plateforme, du parent interface et des modes de fonctionnement.

## 5. DHCP server

Exemple de scope :
```fortios
config system dhcp server
    edit 1
        set interface "VLAN20"
        set default-gateway 198.51.100.1
        set netmask 255.255.255.0
        config ip-range
            edit 1
                set start-ip 198.51.100.100
                set end-ip 198.51.100.200
            next
        end
    next
end
```

Vérifier la configuration DHCP et les baux avec les commandes de diagnostic adaptées à la version.

## 6. Routage statique

```fortios
config router static
    edit 1
        set dst 203.0.113.0 255.255.255.0
        set gateway 192.0.2.254
        set device "port1"
    next
end
```

Une route par défaut utilise `0.0.0.0 0.0.0.0`. Vérifier la table de routage effective, les distances et les routes dynamiques.

## 7. OSPF

Exemple conceptuel :
```fortios
config router ospf
    set router-id 192.0.2.1
    config area
        edit 0.0.0.0
        next
    end
    config network
        edit 1
            set prefix 198.51.100.0 255.255.255.0
        next
    end
end
```

Vérifier les voisins, interfaces OSPF, routes et logs. Adapter les paramètres de zone, authentification et interfaces à la topologie.

## 8. BGP

```fortios
config router bgp
    set as 65001
    set router-id 192.0.2.1
    config neighbor
        edit "203.0.113.1"
            set remote-as 65002
        next
    end
end
```

Ajouter des politiques d'import/export, filtres de préfixes et protections avant toute utilisation réelle. Ne pas annoncer des préfixes non autorisés.

## 9. Objets d'adresses et politiques

Objet :
```fortios
config firewall address
    edit "USERS-NET"
        set subnet 198.51.100.0 255.255.255.0
    next
end
```

Policy illustrative :
```fortios
config firewall policy
    edit 10
        set name "USERS-to-WAN"
        set srcintf "VLAN20"
        set dstintf "port1"
        set srcaddr "USERS-NET"
        set dstaddr "all"
        set action accept
        set schedule "always"
        set service "HTTPS" "DNS"
        set nat enable
    next
end
```

`ALL` n'est pas une bonne valeur par défaut. Définir les services nécessaires. Les policies sont ordonnées ; vérifier leur position et les règles implicites.

## 10. VIP / DNAT

```fortios
config firewall vip
    edit "WEB-HTTPS"
        set extip 203.0.113.10
        set mappedip "198.51.100.10"
        set extintf "port1"
        set portforward enable
        set extport 443
        set mappedport 443
    next
end
```

La policy associée doit autoriser le flux attendu. Vérifier le routage retour, les interfaces et les éventuels conflits.

## 11. NTP et syslog

Les commandes et options exactes varient selon FortiOS. Le principe est de définir des serveurs de temps fiables et un collecteur de logs central, puis de vérifier l'état de synchronisation et la livraison des événements.

Ne pas copier une syntaxe de NTP d'une release vers une autre sans contrôle.

## 12. Sauvegarde et vérification

Sauvegarder la configuration avec une méthode autorisée et sécurisée. Les exports peuvent contenir des secrets et doivent être protégés.

Commandes utiles selon version :
```fortios
get system status
get system interface
get router info routing-table all
get router info ospf neighbor
get router info bgp summary
diagnose sys session list
execute log display
```

## 13. FortiZTP

FortiZTP est distinct de Cisco ZTP et de Cisco AutoInstall. Son fonctionnement peut dépendre de l'enregistrement de l'équipement, de FortiCloud, de FortiManager, du modèle, de la version et des droits associés.

Avant de générer un workflow :
1. identifier le modèle et FortiOS ;
2. confirmer l'éligibilité au mécanisme ;
3. vérifier les prérequis de compte, d'enregistrement et de service ;
4. définir les données de bootstrap ;
5. documenter l'association équipement/site ;
6. tester l'onboarding et le retour d'état.

Cette référence ne décrit pas un workflow FortiZTP universel.
