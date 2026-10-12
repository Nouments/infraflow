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

### Résolution des identifiants (`DeviceCredentialResolver`)
Les identifiants SSH sont résolus au moment de l'exécution par `EnvDeviceCredentialResolver` depuis l'environnement du Provider :
- Spécifique : `INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_USER` et `INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_PASSWORD` / `_SSH_KEY_FILE`
- Global : `INFRAFLOW_CISCO_SSH_USER` et `INFRAFLOW_CISCO_SSH_PASSWORD` / `_SSH_KEY_FILE`

Aucun secret n'apparaît dans les paramètres de tâche, les résultats, les erreurs ou les logs.

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
   username infraflow privilege 15 secret C1sc0InfraFl0w!2026
   line vty 0 4
    login local
    transport input ssh
   exit
   ```
3. Clé d'hôte enregistrée dans `known_hosts` du Provider :
   ```bash
   ssh-keyscan -H 192.168.90.10 >> /var/lib/infraflow-provider/.ssh_known_hosts
   ```

### Procédure de test non destructive
1. Déclarer le routeur dans l'infrastructure du Provider (`management.ipv4: 192.168.90.10`) et générer l'inventaire :
   ```bash
   bin/infraflow-provider generate -f dev-lab/cisco-real.yaml -out /var/lib/infraflow-provider
   ```
2. Exporter les identifiants :
   ```bash
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_USER="infraflow"
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_PASSWORD="C1sc0InfraFl0w!2026"
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
