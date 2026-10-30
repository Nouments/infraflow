# InfraFlow — Référentiel réseau multi-constructeurs

**Documentation Markdown autonome pour consultation hors ligne par les agents InfraFlow.**

- Constructeurs : Cisco IOS / IOS XE, MikroTik RouterOS 7, Fortinet FortiOS.
- Langue : français.
- Version documentaire : 1.0.0
- Statut : référence technique initiale ; les exemples doivent être adaptés et validés sur la plateforme exacte.
- Périmètre : fondamentaux réseau, configurations de référence, vérification, dépannage et provisioning.

## Lire dans cet ordre

1. `01-fondamentaux-reseau.md` — concepts indépendants des constructeurs.
2. `02-methodologie-configuration.md` — méthode commune pour concevoir, appliquer et vérifier une configuration.
3. `cisco/README.md` — Cisco IOS / IOS XE.
4. `mikrotik/README.md` — MikroTik RouterOS 7.
5. `fortinet/README.md` — Fortinet FortiOS / FortiGate.
6. `labs/README.md` — scénarios de laboratoire.
7. `07-glossaire.md` — vocabulaire commun.

## Consignes pour un agent offline

- Considérer cette documentation comme une base de connaissance locale, pas comme une preuve de compatibilité matérielle.
- Toujours identifier le constructeur, le modèle, le système et la version avant de choisir une commande ou un template.
- Ne jamais mélanger les syntaxes IOS, IOS XE, RouterOS 6/7 et FortiOS.
- Ne jamais appliquer un exemple sans vérifier les noms d'interfaces, les fonctionnalités disponibles et les prérequis.
- Ne pas inventer une commande lorsqu'une capacité n'est pas documentée ici.
- Si une information dépend d'une version ou d'un modèle absent, répondre `NON_DOCUMENTE` et demander une source ou une validation.
- Ne jamais exposer de mots de passe, clés privées, community SNMP ou tokens dans les configurations d'exemple, les logs ou les manifestes.
- Les adresses `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` et `2001:db8::/32` sont réservées à la documentation. Les remplacer dans un vrai déploiement.
- Les commandes de remise à zéro, restauration, mise à jour ou réinstallation sont potentiellement destructives.

## Statut des exemples

Les blocs de configuration sont des **exemples pédagogiques**. Ils ne sont pas tous universels. Un exemple n'est pas une configuration de production, ni une preuve que la fonction existe sur chaque équipement.

## Limites de cette édition

Cette édition décrit les fondamentaux et les syntaxes de référence fournies pour les trois constructeurs. Elle ne constitue pas une matrice exhaustive de compatibilité par modèle/version. Les workflows ZTP, AutoInstall, FortiZTP et provisioning RouterOS doivent être documentés séparément pour chaque plateforme prise en charge.
