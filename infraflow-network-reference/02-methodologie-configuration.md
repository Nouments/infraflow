# 02 — Méthodologie commune de configuration et de dépannage

## 1. Identifier la cible avant de générer

Toute opération doit connaître autant que possible :
- vendor ;
- modèle/famille ;
- OS et version exacte ;
- rôle réseau ;
- interfaces disponibles et leurs noms ;
- capacités matérielles/licences ;
- mode d'exploitation (L2, L3, firewall, routeur, contrôleur) ;
- état initial et accès de gestion.

Si une donnée critique manque, ne pas supposer un nom d'interface ou une capacité.

## 2. Concevoir avant de configurer

Pour chaque site, produire :
1. inventaire des équipements ;
2. schéma logique et physique ;
3. plan IPv4/IPv6 ;
4. plan VLAN ;
5. plan de routage ;
6. matrice des flux ;
7. politique de sécurité ;
8. stratégie de gestion hors bande ;
9. stratégie de sauvegarde et rollback ;
10. plan de validation.

## 3. Ordre recommandé d'implémentation

1. accès console/hors bande et identité ;
2. interfaces physiques et lien ;
3. adressage IP ;
4. VLAN et trunks ;
5. routage local ;
6. routage dynamique ;
7. ACL et politiques firewall ;
8. NAT ;
9. services d'infrastructure (DHCP, DNS relay, NTP, syslog) ;
10. supervision et sauvegarde ;
11. HA et QoS ;
12. vérification de bout en bout.

Cet ordre est indicatif : respecter les dépendances et la fenêtre de changement du site.

## 4. Gestion des changements

Avant application :
- récupérer la configuration actuelle ;
- vérifier l'accès de secours ;
- sauvegarder ;
- calculer le diff ;
- identifier les changements destructifs ;
- prévoir une fenêtre et un rollback ;
- limiter le périmètre du changement.

Après application :
- vérifier l'état des interfaces ;
- vérifier routes et voisins ;
- tester les flux autorisés ;
- tester les flux interdits ;
- vérifier logs et supervision ;
- sauvegarder la configuration validée.

## 5. Secrets

Ne pas placer de secrets en clair dans :
- fichiers YAML versionnés ;
- templates ;
- exemples ;
- logs ;
- rapports ;
- manifestes d'artefacts.

Utiliser un mécanisme de secrets adapté. Les templates doivent référencer les secrets sans les imprimer. Utiliser SSH avec vérification de clé d'hôte lorsque disponible. SNMPv3 est préférable à une community SNMPv1/v2c.

## 6. Diagnostic par couches

| Étape | Question | Contrôles |
|---|---|---|
| L1 | Le lien est-il actif ? | câble, optique, état, erreurs |
| L2 | Le VLAN et la MAC sont-ils corrects ? | VLAN, trunk, table MAC, STP |
| L3 | L'IP et la route sont-elles correctes ? | adresse, masque, ARP/NDP, route |
| L4 | Le port/service est-il joignable ? | TCP/UDP, ACL, firewall |
| L7 | Le service répond-il ? | DNS, HTTP, authentification, logs |

Ne pas commencer par modifier le firewall si le lien physique ou l'adressage est incorrect.

## 7. Commandes de vérification génériques

Depuis un hôte Linux :
```bash
ip address
ip route
ip neigh
ping -c 4 192.0.2.1
tracepath 203.0.113.10
dig example.invalid
curl -vk https://192.0.2.1/
ss -tulpn
```

Ces commandes sont des outils d'observation. Les résultats doivent être interprétés en tenant compte du routage, des ACL et du contexte de test.
