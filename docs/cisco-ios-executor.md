# Exécuteur Cisco IOS via SSH (`cisco.ios.configure`)

L'exécuteur `cisco.ios.configure` implémente le port `ports.ExecutionExecutor` pour appliquer et vérifier de manière typée et sécurisée la configuration d'un équipement Cisco IOS via SSH.

---

## 1. Périmètre fonctionnel (Version 1)

L'exécuteur traite exclusivement :
- Le nom d'hôte (`hostname`) ;
- Une ou plusieurs interfaces Ethernet (`interfaces`) :
  - Nom canonique de l'interface (ex: `GigabitEthernet0/0`) ;
  - Description de l'interface (texte ASCII sans retour à la ligne ni caractère de contrôle) ;
  - Adresse IPv4 avec préfixe CIDR (ex: `192.168.10.1/24`) ;
  - État administratif demandé (`up` ou `down`).
- La relecture et vérification obligatoire de la configuration active (`show running-config`).

### Limites explicites
- **Persistance :** Par défaut, aucune commande `write memory` ou `copy running-config startup-config` n'est exécutée. La configuration n'est appliquée et vérifiée que dans la `running-config`.
- **Sortie SSH :** Chaque réponse de commande est limitée à 1 MiB en mémoire. Cette limite V1 couvre les commandes fixes de réglage du terminal, les commandes de configuration typées et la relecture d'une interface (jusqu'à 32 interfaces par tâche). Une sortie qui dépasse la limite ferme la session et fait échouer la tâche; aucune sortie tronquée n'est acceptée comme preuve de vérification. Une lecture complète de la configuration du routeur n'est pas une commande prise en charge.
- **Retries :** `cisco.ios.configure` refuse les politiques de retry automatique et n'est exécuté qu'une fois. Après toute erreur survenue lorsqu'une mutation a pu commencer, l'état du routeur doit être relu avant une nouvelle soumission. Aucun rollback n'est annoncé ou tenté.
- **Fonctionnalités réseau exclues en V1 :** Aucun support pour NAT, ACL, routage dynamique (BGP/OSPF), VLANs/switchport, ou équipements d'autres constructeurs (FortiGate, MikroTik, Proxmox).

---

## 2. Contrat JSON des paramètres

```json
{
  "site": "cisco-lab",
  "device_id": "R1",
  "hostname": "R1-CORE",
  "interfaces": [
    {
      "name": "GigabitEthernet2",
      "description": "InfraFlow Lab Uplink",
      "ipv4_cidr": "10.99.1.1/30",
      "admin_state": "up"
    }
  ]
}
```

### Règles de validation
- Rejet strict des champs inconnus (`DisallowUnknownFields`).
- Rejet des charges vides ou avec données résiduelles.
- Validation des identifiants `site` et `device_id` selon la nomenclature InfraFlow.
- Rejet des noms d'interfaces malformés ou d'un type non pris en charge.
- Canonisation automatique des préfixes courts (`Gi0/0` -> `GigabitEthernet0/0`, `Fa0/0` -> `FastEthernet0/0`, `Eth0/0` -> `Ethernet0/0`).
- Rejet des interfaces dupliquées (insensible à la casse).
- Validation stricte de l'IPv4 CIDR (rejet d'IPv6, multicast, broadcast, loopback, 0.0.0.0).
- Rejet des retours à la ligne ou caractères de contrôle dans les descriptions.
- Rejet des commandes shell ou IOS brutes.
- Rejet de toute adresse IP de destination dans le payload pour empêcher le contournement d'autorisation.

---

## 3. Résolution de cible et secrets

### Résolution de cible (`DeviceTargetResolver`)
La cible doit exister dans l'inventaire du site (`<site>/inventory.json`) publié dans le catalogue du Provider (`ArtifactInventoryDeviceResolver`). Les cibles inconnues ou dont le constructeur n'est pas Cisco sont rejetées avec l'erreur `target device is not authorized or not found`.

Avant toute connexion, le resolver vérifie que le site du document correspond au site demandé, que les identités d'équipement sont présentes et non ambiguës, que l'équipement correspond à une entrée Cisco et que son IPv4 de gestion est valide. L'inventaire est une source de données d'autorisation locale; il n'est pas signé ni vérifié cryptographiquement par ce contrôle. Une absence, un JSON malformé, un site incohérent, une identité ambiguë, un équipement non Cisco ou une adresse de gestion invalide bloque la connexion.

### Résolution des identifiants (`DeviceCredentialResolver`)
Les identifiants SSH sont résolus au moment de l'exécution par `EnvDeviceCredentialResolver` depuis l'environnement du Provider :
- Spécifique : `INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_USER` et `INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_PASSWORD` / `_SSH_KEY_FILE`
- Global : `INFRAFLOW_CISCO_SSH_USER` et `INFRAFLOW_CISCO_SSH_PASSWORD` / `_SSH_KEY_FILE`

Aucun secret n'apparaît dans les paramètres de tâche, les résultats, les erreurs ou les logs.
Ne placez jamais de secret réel dans le YAML d'infrastructure, le JSON de tâche/résultat, une commande conservée, l'historique shell ou les logs. Injectez les valeurs depuis un gestionnaire de secrets ou l'environnement protégé du processus Provider; les valeurs entre chevrons ci-dessous sont des placeholders inutilisables.

### Politique de clé d'hôte (`HostKeyVerifier`)
InfraFlow vérifie l'identité du serveur via `known_hosts` (`KnownHostsFileVerifier`) ou empreinte SHA-256 (`FixedFingerprintVerifier`). Toute clé inconnue ou modifiée entraîne un échec immédiat.

---

## 4. Relecture et vérification

Après exécution des commandes et sortie du mode configuration (`end`), l'exécuteur relit :
- `show running-config | include ^hostname`
- `show running-config interface <name>`

La tâche n'est déclarée `succeeded` qu'après confirmation que l'état observé correspond exactement à la demande. En cas de divergence, la tâche échoue avec un diagnostic explicite.

---

## 5. Procédure de validation en laboratoire GNS3 réel

> **Statut de la validation lab en direct : NON EXÉCUTÉ**  
> Justification : Aucun banc GNS3 actif ni routeur Cisco IOS physique/virtuel n'est connecté au réseau local pendant l'exécution courante. Conformément aux règles d'InfraFlow, aucun résultat de laboratoire n'est simulé ou inventé.

### Prérequis sur le routeur Cisco IOS
1. Cisco IOS ou IOS XE avec support SSH v2 :
   ```ios
   hostname R1
   ip domain-name infraflow.local
   crypto key generate rsa modulus 2048
   ip ssh version 2
   ```
2. Compte de test (privilège 15) :
   ```ios
   username infraflow privilege 15 secret <MOT_DE_PASSE_DE_TEST>
   line vty 0 4
    login local
    transport input ssh
   exit
   ```
3. Vérifier la clé d'hôte par un canal indépendant avant approbation :
   ```bash
   ssh-keyscan -H 192.168.90.10
   ```
   `ssh-keyscan` ne prouve pas l'identité du routeur. Obtenir l'empreinte SHA-256 attendue depuis la console du routeur, un inventaire d'administration authentifié ou un autre canal indépendant, puis la comparer à l'empreinte de la clé candidate récupérée sur le réseau. N'approuver/ajouter cette clé à `known_hosts` qu'après correspondance exacte et vérification de l'adresse/équipement. Toute clé inconnue ou modifiée doit faire échouer la connexion; ne désactivez jamais la vérification de clé d'hôte.

### Procédure de test non destructive
1. Déclarer le routeur dans l'infrastructure du Provider (`management.ipv4: 192.168.90.10`) et générer l'inventaire :
   ```bash
   bin/infraflow-provider generate -f dev-lab/cisco-real.yaml -out /var/lib/infraflow-provider
   ```
2. Injecter les identifiants depuis un gestionnaire de secrets ou un environnement protégé. Ne tapez pas de secret réel dans une commande enregistrée; le placeholder suivant n'est pas un identifiant valide :
   ```bash
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_USER="infraflow"
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_PASSWORD="<MOT_DE_PASSE_DE_TEST>"
   ```
3. Relever l'état initial sur le routeur :
   ```ios
   show version
   show running-config | include ^hostname
   show running-config interface GigabitEthernet2
   ```
4. Soumettre la tâche via l'API d'exécution :
   ```bash
   curl -X POST http://localhost:8080/api/v1/executions \
     -H "Authorization: Bearer $SESSION_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"tasks":[{"id":"cisco-01","type":"cisco.ios.configure","parameters":{"site":"cisco-lab","device_id":"R1","hostname":"R1-CORE","interfaces":[{"name":"GigabitEthernet2","description":"InfraFlow Test Uplink","ipv4_cidr":"10.99.1.1/30","admin_state":"up"}]}}]}'
   ```
5. Relever l'état final sur le routeur :
   ```ios
   show running-config | include ^hostname
   show running-config interface GigabitEthernet2
   ```
6. Vérifier que la tâche est marquée `succeeded`, avec `verified: true`, et archiver le JSON de preuve.

Après validation, supprimer les variables de test du shell et redémarrer le Provider si son processus avait hérité de ces variables :

```bash
unset INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_USER
unset INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_PASSWORD
unset INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_KEY_FILE
unset INFRAFLOW_CISCO_SSH_USER INFRAFLOW_CISCO_SSH_PASSWORD INFRAFLOW_CISCO_SSH_KEY_FILE
```
