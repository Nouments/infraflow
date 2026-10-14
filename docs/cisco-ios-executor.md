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
> Lors de cette tâche, aucun processus/socket GNS3 local ni API sur `127.0.0.1:3080` n'était disponible. Aucune adresse Cisco n'a été contactée. Les vérifications CLI ci-dessous portent uniquement sur l'exemple documentaire; elles ne sont pas une exécution IOS.

> **Mise à jour 2026-10-14 :** un processus `gns3server` local et son listener `127.0.0.1:3080` ont été observés. Le processus QEMU observé était nommé `fortigate-1`, hors périmètre; aucun nœud Cisco autorisé, son adresse de gestion ou son empreinte SSH indépendante n'a été confirmé. Un GET local vers `/v2/projects` a retourné HTTP 404. Aucune connexion à un routeur n'a été tentée. Utiliser le [runbook de lab Cisco IOS](lab/cisco-ios-gns3.md) pour la procédure actuelle; les cas de configuration restent NON EXÉCUTÉS.

### Prérequis et préparation
1. Utiliser un routeur IOS/IOS XE dédié à un lab GNS3 isolé et une interface de test non reliée à la production. L'adresse de gestion doit être joignable depuis l'hôte Provider. Cette procédure n'assigne ni ne modifie l'adresse IPv4 de l'interface testée.
2. Activer SSH v2 et générer une clé hôte si nécessaire :
   ```ios
   hostname R1
   ip domain-name infraflow.local
   crypto key generate rsa modulus 2048
   ip ssh version 2
   ```
3. Créer un compte IOS de test avec privilège 15. L'exécuteur ouvre directement un shell privilégié et entre en mode configuration; il ne lance pas `enable` :
   ```ios
   username infraflow privilege 15 secret <REMPLACER_PAR_UN_SECRET_DE_TEST>
   line vty 0 4
    login local
    transport input ssh
    exec-timeout 10 0
   exit
   ```
   Le texte entre chevrons est un placeholder, pas un mot de passe à configurer. Ne mettez aucun vrai secret dans un fichier suivi par Git, un JSON de tâche, une commande enregistrée ou un log.
4. Copier l'exemple vers un fichier local non versionné, puis remplacer l'adresse TEST-NET par l'adresse de gestion réelle du routeur de lab :
   ```bash
   cp examples/cisco-ios-lab.yaml /tmp/cisco-ios-lab.local.yaml
   ${EDITOR:-vi} /tmp/cisco-ios-lab.local.yaml
   ```
   [cisco-ios-lab.yaml](../examples/cisco-ios-lab.yaml) utilise `192.0.2.10` (TEST-NET-1) comme exemple non routable. Ne l'utilisez jamais pour une connexion réelle. Ce YAML ne contient aucun secret.
5. Définir un répertoire d'artefacts absolu, identique pour `generate` et `serve`, puis valider et générer l'inventaire :
   ```bash
   make build-provider
   export LAB_DATA_DIR="$HOME/.local/share/infraflow/cisco-lab"
   mkdir -p "$LAB_DATA_DIR"
   bin/infraflow-provider validate -f /tmp/cisco-ios-lab.local.yaml
   bin/infraflow-provider generate -f /tmp/cisco-ios-lab.local.yaml -out "$LAB_DATA_DIR"
   jq '.site, (.devices[] | select(.name == "R1"))' "$LAB_DATA_DIR/cisco-lab/inventory.json"
   ```
   `generate` publie `cisco-lab/inventory.json` sous le répertoire de sortie. `ArtifactInventoryDeviceResolver` autorise le couple site/équipement à partir de ce fichier; il n'est ni signé ni vérifié cryptographiquement.
6. Obtenir l'empreinte attendue depuis la console IOS ou un autre canal d'administration indépendant. `ssh-keyscan` ne prouve pas l'identité du routeur : il ne fournit qu'une clé candidate. Après comparaison exacte de l'empreinte SHA-256 et validation de l'adresse/équipement, ajouter la clé candidate au fichier réellement utilisé par InfraFlow :
   ```bash
   export ROUTER_IP=<ADRESSE_DE_GESTION_DU_LAB>
   export KNOWN_HOSTS="$LAB_DATA_DIR/.ssh_known_hosts"
   CANDIDATE_KEY="$(mktemp)"
   ssh-keyscan -T 5 -t rsa "$ROUTER_IP" > "$CANDIDATE_KEY"
   ssh-keygen -lf "$CANDIDATE_KEY" -E sha256
   # Comparer cette empreinte à celle obtenue par le canal indépendant.
   cat "$CANDIDATE_KEY" >> "$KNOWN_HOSTS"
   chmod 600 "$KNOWN_HOSTS"
   rm -f "$CANDIDATE_KEY"
   ```
   N'ajoutez la clé qu'après cette comparaison. Si une entrée existe déjà, vérifiez-la au lieu de l'écraser. Toute clé inconnue ou modifiée doit faire échouer la connexion; ne désactivez jamais la vérification de clé d'hôte.

### Démarrer le Provider et ouvrir une session admin
1. Créer une configuration locale `serve`, en gardant les deux écouteurs sur loopback et le même répertoire d'artefacts :
   ```bash
   cat > /tmp/provider-cisco-lab.yaml <<YAML
   listen_address: 127.0.0.1:50051
   api_listen_address: 127.0.0.1:8080
   web_ui_enabled: false
   artifact_directory: $LAB_DATA_DIR
   database_path: $LAB_DATA_DIR/.infraflow-users.sqlite3
   token_env: INFRAFLOW_AGENT_TOKEN
   admin_username: admin
   admin_credential_file: $LAB_DATA_DIR/.infraflow-admin-password
   admin_credential_script: $LAB_DATA_DIR/get-admin-password.sh
   known_hosts_file: $LAB_DATA_DIR/.ssh_known_hosts
   YAML
   ```
   Les autres paramètres peuvent reprendre [provider-config.yaml](../examples/provider-config.yaml). Une écoute HTTP sur loopback est locale; toute exposition distante exige le TLS du Provider.
2. Générer un token d'agent aléatoire sans l'afficher, puis démarrer le Provider :
   ```bash
   export INFRAFLOW_AGENT_TOKEN="$(openssl rand -hex 32)"
   bin/infraflow-provider serve -config /tmp/provider-cisco-lab.yaml
   ```
   Au premier démarrage avec une base vide, le Provider écrit le mot de passe admin généré dans le fichier configuré et fournit un script de lecture (par défaut `"$LAB_DATA_DIR/get-admin-password.sh"`). Une base existante n'est pas réinitialisée : utilisez son compte admin déjà provisionné.
3. Résoudre les identifiants IOS depuis un gestionnaire de secrets ou une saisie interactive qui ne les place pas dans l'historique :
   ```bash
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_USER=infraflow
   read -r -s -p 'Mot de passe IOS de test: ' IOS_TEST_PASSWORD
   printf '\n'
   export INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_PASSWORD="$IOS_TEST_PASSWORD"
   unset IOS_TEST_PASSWORD
   ```
4. Ouvrir une session administrateur via la vraie route login. Le mot de passe passe à `curl` par stdin, pas dans ses arguments :
   ```bash
   API=http://127.0.0.1:8080
   ADMIN_PASSWORD="$("$LAB_DATA_DIR/get-admin-password.sh")"
   LOGIN_RESPONSE="$(curl --silent --show-error --fail-with-body \
     -H 'Content-Type: application/json' --data-binary @- \
     "$API/api/v1/auth/login" <<JSON
   {"username":"admin","password":"$ADMIN_PASSWORD"}
   JSON
   )"
   SESSION_TOKEN="$(printf '%s' "$LOGIN_RESPONSE" | jq -er '.token')"
   unset ADMIN_PASSWORD LOGIN_RESPONSE
   umask 077
   CURL_AUTH="$(mktemp)"
   printf 'header = "Authorization: Bearer %s"\n' "$SESSION_TOKEN" > "$CURL_AUTH"
   chmod 600 "$CURL_AUTH"
   ```
   Le fichier temporaire contient le token de session : mode `0600`, suppression en fin de procédure. `POST /api/v1/executions` exige un administrateur et un header `Idempotency-Key` de 16 à 128 caractères.

### Capturer, appliquer, vérifier et restaurer
1. Depuis la console IOS, sélectionner une interface dédiée au test, vérifier qu'elle ne transporte aucun trafic de production et enregistrer sa description et son état administratif initiaux :
   ```ios
   show version
   show running-config | include ^hostname
   show running-config interface <INTERFACE_DE_TEST>
   show interfaces <INTERFACE_DE_TEST>
   ```
   Dans la configuration IOS, l'état administratif est `down` si `shutdown` apparaît, sinon `up`. Conserver exactement la description initiale et la définir dans le shell comme `INITIAL_DESCRIPTION`; utiliser une chaîne vide si elle était absente.
2. Remplacer les valeurs en chevrons par l'interface et l'état réellement observés, puis soumettre une tâche qui ne change que la description. `ipv4_cidr` et `hostname` sont volontairement omis :
   ```bash
   TEST_INTERFACE=<INTERFACE_DE_TEST>
   INITIAL_ADMIN_STATE=<up_ou_down>
   TEST_DESCRIPTION=InfraFlow-GNS3-reversible-test
   REQUEST_KEY="cisco-ios-lab-change-$(date +%s)"
   jq -n --arg iface "$TEST_INTERFACE" --arg admin "$INITIAL_ADMIN_STATE" --arg desc "$TEST_DESCRIPTION" \
     '{tasks:[{id:"cisco-lab-change",type:"cisco.ios.configure",timeout_ms:30000,max_attempts:1,parameters:{site:"cisco-lab",device_id:"R1",interfaces:[{name:$iface,description:$desc,admin_state:$admin}]}}]}' |
    SUBMISSION="$(curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
     -H 'Content-Type: application/json' -H "Idempotency-Key: $REQUEST_KEY" \
       --data-binary @- "$API/api/v1/executions")"
    printf '%s\n' "$SUBMISSION" | jq '{id,status,tasks:[.tasks[]|{id,type,status}]}'
    EXECUTION_ID="$(printf '%s' "$SUBMISSION" | jq -er '.id')"
   ```
    La modification est uniquement en `running-config`; aucune commande `write memory` n'est exécutée.
3. Poller la ressource jusqu'à un état terminal et conserver le JSON réel hors du dépôt :
   ```bash
   EXECUTION_ID=<ID_RETOURNE>
   PROOF_DIR="$HOME/.local/share/infraflow/cisco-lab/proof"
   install -d -m 700 "$PROOF_DIR"
   while :; do
     curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
       "$API/api/v1/executions/$EXECUTION_ID" > "$PROOF_DIR/execution.json"
     EXECUTION_STATUS="$(jq -er '.status' "$PROOF_DIR/execution.json")"
     case "$EXECUTION_STATUS" in succeeded|failed|cancelled|timed_out) break ;; *) sleep 1 ;; esac
   done
   jq '{id,status,tasks:[.tasks[]|{id,type,status,attempts,result,error}]}' "$PROOF_DIR/execution.json"
   ```
   La route est `GET /api/v1/executions/{id}`; le résultat Cisco est dans `tasks[].result`, et les tentatives dans `tasks[].attempts`. Ne conclure à un succès que si l'exécution et la tâche sont `succeeded` et `tasks[].result.verified` vaut `true`.
4. Relire l'interface dans la console, puis restaurer la description et l'état administratif initiaux avec une nouvelle tâche et une nouvelle clé d'idempotence :
    ```bash
    RESTORE_KEY="cisco-ios-lab-restore-$(date +%s)"
    jq -n --arg iface "$TEST_INTERFACE" --arg admin "$INITIAL_ADMIN_STATE" --arg desc "$INITIAL_DESCRIPTION" \
       '{tasks:[{id:"cisco-lab-restore",type:"cisco.ios.configure",timeout_ms:30000,max_attempts:1,parameters:{site:"cisco-lab",device_id:"R1",interfaces:[{name:$iface,description:$desc,admin_state:$admin}]}}]}' |
      RESTORE_SUBMISSION="$(curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
       -H 'Content-Type: application/json' -H "Idempotency-Key: $RESTORE_KEY" \
          --data-binary @- "$API/api/v1/executions")"
      RESTORE_ID="$(printf '%s' "$RESTORE_SUBMISSION" | jq -er '.id')"
      while :; do
         curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
            "$API/api/v1/executions/$RESTORE_ID" > "$PROOF_DIR/restoration.json"
         RESTORE_STATUS="$(jq -er '.status' "$PROOF_DIR/restoration.json")"
         case "$RESTORE_STATUS" in succeeded|failed|cancelled|timed_out) break ;; *) sleep 1 ;; esac
      done
      jq '{id,status,tasks:[.tasks[]|{id,type,status,attempts,result,error}]}' "$PROOF_DIR/restoration.json"
    ```
    Une description vide devient `no description`; `ipv4_cidr` reste omis pour préserver l'adresse. Relire l'interface après restauration.
5. Archiver les réponses JSON réelles et les sorties IOS initiales/finales avec la date, la version IOS et l'empreinte approuvée. Ne jamais mettre de mot de passe, token ou clé privée dans les preuves.

### Échec contrôlé et nettoyage
Pour vérifier un échec de connexion sans mutation après la restauration :

1. Arrêter le Provider normal, créer un fichier de confiance de test distinct et une copie de sa configuration. Ne pas modifier `.ssh_known_hosts` :
    ```bash
    UNTRUSTED_HOSTS="$LAB_DATA_DIR/.ssh_known_hosts-untrusted"
    printf '# Deliberately empty trust store for controlled failure test\n' > "$UNTRUSTED_HOSTS"
    chmod 600 "$UNTRUSTED_HOSTS"
    cp /tmp/provider-cisco-lab.yaml /tmp/provider-cisco-lab-untrusted.yaml
    ${EDITOR:-vi} /tmp/provider-cisco-lab-untrusted.yaml
    ```
    Dans la copie, changer uniquement `known_hosts_file` vers la valeur de `$UNTRUSTED_HOSTS`; garder le même `artifact_directory` et la même base SQLite. Redémarrer le Provider avec cette copie. Les sessions admin sont persistées dans SQLite et le token déjà obtenu reste utilisable jusqu'à expiration.
2. Soumettre la même configuration, avec une nouvelle clé d'idempotence, puis poller `GET /api/v1/executions/{id}` comme précédemment. La connexion doit échouer sur clé d'hôte inconnue avant authentification; la ressource doit être `failed` avec une seule tentative et l'état de l'interface doit rester inchangé :
    ```bash
    FAILURE_KEY="cisco-ios-lab-untrusted-host-$(date +%s)"
    jq -n --arg iface "$TEST_INTERFACE" --arg admin "$INITIAL_ADMIN_STATE" --arg desc "$TEST_DESCRIPTION" \
       '{tasks:[{id:"cisco-lab-untrusted-host",type:"cisco.ios.configure",timeout_ms:30000,max_attempts:1,parameters:{site:"cisco-lab",device_id:"R1",interfaces:[{name:$iface,description:$desc,admin_state:$admin}]}}]}' |
    FAILURE_SUBMISSION="$(curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
       -H 'Content-Type: application/json' -H "Idempotency-Key: $FAILURE_KEY" \
       --data-binary @- "$API/api/v1/executions")"
    FAILURE_ID="$(printf '%s' "$FAILURE_SUBMISSION" | jq -er '.id')"
    while :; do
       curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
          "$API/api/v1/executions/$FAILURE_ID" > "$PROOF_DIR/untrusted-host.json"
       FAILURE_STATUS="$(jq -er '.status' "$PROOF_DIR/untrusted-host.json")"
       case "$FAILURE_STATUS" in failed|succeeded|cancelled|timed_out) break ;; *) sleep 1 ;; esac
    done
    jq -e '.tasks[0].status == "failed" and (.tasks[0].attempts | length) == 1' "$PROOF_DIR/untrusted-host.json"
    ```
    Arrêter l'instance de test puis redémarrer le Provider normal avec le fichier de confiance approuvé.

Ce test contrôle un échec avant mutation; il ne simule pas une coupure après mutation. Le timeout après une commande de configuration est couvert par les tests automatisés injectés. Une requête Cisco demandant `max_attempts > 1` doit être rejetée en HTTP `400` avant exécution.

Arrêter le Provider, supprimer le fichier temporaire d'authentification, et retirer les variables de test. Redémarrer le Provider est nécessaire pour retirer de son environnement les secrets déjà hérités par le processus :
```bash
rm -f "$CURL_AUTH"
unset SESSION_TOKEN INFRAFLOW_AGENT_TOKEN
unset INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_USER INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_PASSWORD
unset INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_KEY_FILE INFRAFLOW_DEVICE_CISCO_LAB_R1_SSH_KEY_PASSPHRASE
unset INFRAFLOW_CISCO_SSH_USER INFRAFLOW_CISCO_SSH_PASSWORD
unset INFRAFLOW_CISCO_SSH_KEY_FILE INFRAFLOW_CISCO_SSH_KEY_PASSPHRASE
```
