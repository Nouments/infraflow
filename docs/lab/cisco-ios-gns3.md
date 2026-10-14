# Validation de `cisco.ios.configure` dans un lab GNS3

**Statut de cette passe : NON EXÉCUTÉ sur Cisco IOS.** Aucun nœud Cisco IOS avec adresse de gestion autorisée et empreinte de clé approuvée n'a été confirmé. Le serveur GNS3 local était actif, mais le seul processus de VM observé était nommé `fortigate-1`, hors périmètre. Aucun SSH ou appel API de configuration n'a été adressé à un équipement.

Ce document est un runbook pour l'opérateur du lab. Les sorties et statuts attendus ci-dessous sont des critères, pas des preuves déjà obtenues.

## Topologie et adresses connues

Le segment Docker/GNS3 documenté dans [README.md](../../README.md) utilise `192.168.0.0/24`, avec Agent `192.168.0.2/24` et Provider `192.168.0.3/24`. Ces adresses sont celles des services InfraFlow, pas celles d'un routeur IOS.

[examples/cisco-ios-lab.yaml](../../examples/cisco-ios-lab.yaml) déclare `R1` avec `192.0.2.10`; le commentaire du fichier précise que c'est une adresse TEST-NET documentaire, non routable et non utilisable pour se connecter. Aucun autre management IP Cisco n'est défini par cet exemple. Ne déduisez pas l'adresse IOS de celles de l'Agent ou du Provider. L'opérateur doit confirmer dans le projet GNS3 le nœud IOS exact, son interface de management et son adresse avant de poursuivre.

Topologie à confirmer dans GNS3 avant le test : Provider InfraFlow et un routeur Cisco IOS/IOS XE dédié reliés à un réseau isolé; seule l'interface Ethernet de test est configurée. Aucun lien vers un réseau de production. Le VLAN, le sous-réseau Cisco, l'adresse du routeur et les valeurs à appliquer ne sont pas spécifiés ici, car aucune valeur de ce type n'est prouvée par README ou l'exemple Cisco.

## Prérequis

- Un projet GNS3 identifié par l'opérateur, un nœud Cisco IOS/IOS XE démarré, son modèle et sa version IOS observés dans `show version`, et une autorisation explicite pour ce nœud.
- Un accès console initial pour relever l'état et confirmer l'empreinte SSH par un canal indépendant.
- SSH v2 activé sur le routeur, utilisateur de lab privilégié fourni par le gestionnaire de secrets, et clé publique de cet utilisateur chargée dans `ssh-agent`.
- Provider configuré pour n'autoriser que le nœud Cisco déclaré dans son inventaire et configuré avec la vérification stricte de la clé d'hôte. Utiliser une session administrateur pour `/api/v1/executions`.
- Une interface Ethernet dédiée et non utilisée par le trafic de gestion ou de production. Relever son nom, sa description, son adresse/mask et son état administratif avant toute tâche.
- `bash`, OpenSSH (`ssh`, `ssh-keyscan`, `ssh-keygen`), `curl`, `jq` et `scripts/cisco-ios-preflight.sh`.

Les identifiants ne sont jamais dans le YAML, la requête JSON, les commandes sauvegardées, les preuves ni le dépôt. Fournir les secrets au processus Provider depuis le gestionnaire prévu. Ne pas activer la persistance startup-config; l'exécuteur ne lance pas `write memory`.

## Pré-vol en lecture seule

Obtenir l'empreinte SHA-256 attendue depuis la console IOS ou un canal administratif indépendant. La clé retournée par `ssh-keyscan` n'est qu'une clé candidate; le script refuse de continuer si elle ne correspond pas à l'empreinte attendue. Le script n'accepte pas de mot de passe SSH : utiliser une clé déjà chargée dans `ssh-agent`.

Depuis l'hôte réellement utilisé par le Provider, après avoir défini les trois valeurs à partir des informations autorisées du lab :

```bash
IOS_HOST='<adresse-confirmée-par-opérateur>'
IOS_SSH_USER='<utilisateur-de-lab>'
IOS_HOSTKEY_FINGERPRINT='SHA256:<empreinte-confirmée-hors-bande>'
bash scripts/cisco-ios-preflight.sh "$IOS_HOST" "$IOS_SSH_USER" "$IOS_HOSTKEY_FINGERPRINT"
```

Le script récupère une clé candidate, vérifie l'empreinte, puis exécute uniquement `show version` avec authentification SSH par clé, `BatchMode=yes` et contrôle strict de clé d'hôte. Il ne modifie pas `known_hosts` et ne configure pas le routeur. Une sortie `PASS` vérifie uniquement la joignabilité SSH, l'empreinte et la détection d'une ligne de version; elle ne prouve ni une configuration ni la compatibilité de l'exécuteur.

Avant les requêtes, ouvrir la console IOS et enregistrer, sans données d'authentification :

```text
show version
show running-config | include ^hostname
show running-config interface <interface-de-test-confirmée>
show ip interface brief
```

Conserver la sortie horodatée dans un répertoire de preuves protégé hors du dépôt. Relever précisément la description, l'adresse IPv4/masque et `up`/`down` de l'interface.

## Requête et lecture du résultat

L'API exige une session utilisateur `admin`, un header `Idempotency-Key` de 16 à 128 caractères, un `id` de tâche et `type: cisco.ios.configure`. Le résultat de `POST /api/v1/executions` est asynchrone; interroger `GET /api/v1/executions/{id}` jusqu'à un état terminal. Consulter `tasks[].result`; ne déclarer le succès que si l'exécution et la tâche sont `succeeded` et que `result.verified` vaut `true`.

Utiliser un fichier curl de permissions `0600` situé hors du dépôt pour la session bearer, selon le mécanisme admin sécurisé déjà documenté dans [docs/cisco-ios-executor.md](../cisco-ios-executor.md). Écrire les réponses dans le répertoire de preuves protégé, pas dans le dépôt. Les extraits `jq` ci-dessous produisent un JSON de tâche sans secret.

Chaque requête d'exécution utilise cette forme de soumission, avec un header d'idempotence neuf :

```bash
jq -n --arg site "$LAB_SITE" --arg device "$LAB_DEVICE" \
  --arg iface "$LAB_INTERFACE" --arg admin "$INITIAL_ADMIN_STATE" \
  --arg description "$INITIAL_DESCRIPTION" --arg hostname "$NEW_HOSTNAME" \
  '{tasks:[{id:"cisco-hostname-lab",type:"cisco.ios.configure",timeout_ms:30000,max_attempts:1,parameters:{site:$site,device_id:$device,hostname:$hostname,interfaces:[{name:$iface,description:$description,admin_state:$admin}]}}]}' |
  curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
    -H 'Content-Type: application/json' \
    -H "Idempotency-Key: cisco-ios-hostname-$(date +%s)" \
    --data-binary @- "$API/api/v1/executions"
```

Les variables `LAB_SITE`, `LAB_DEVICE`, `LAB_INTERFACE`, `INITIAL_ADMIN_STATE`, `INITIAL_DESCRIPTION` et `NEW_HOSTNAME` doivent provenir de l'inventaire autorisé et de l'état relevé, jamais d'une valeur supposée. Le champ `interfaces` est obligatoire même pour changer le hostname. Dans l'implémentation actuelle, une description vide produit `no description`; envoyer donc la description initiale exacte. L'adresse n'est pas envoyée dans ce cas et n'est pas modifiée.

Pour poller, extraire l'`id` renvoyé par le POST sans afficher les tokens, puis exécuter :

```bash
curl --config "$CURL_AUTH" --silent --show-error --fail-with-body \
  "$API/api/v1/executions/$EXECUTION_ID" > "$PROOF_DIR/hostname-execution.json"
jq '{id,status,tasks:[.tasks[]|{id,type,status,result,error}]}' \
  "$PROOF_DIR/hostname-execution.json"
```

Adapter un nom de fichier distinct par cas; recommencer le GET jusqu'à ce que `status` soit terminal. Les sorties réelles du POST, du GET et de la console IOS sont les preuves, pas les critères attendus de ce runbook.

## Cas de validation

### A. Changement du hostname

Avant : enregistrer `show version`, le hostname courant et l'état complet de l'interface de test avec les commandes ci-dessus. Choisir un hostname de lab approuvé, sans conflit, et préserver description/état administratif initiaux dans la requête.

Action : soumettre `cisco.ios.configure` avec `hostname` et l'interface obligatoire dont description et `admin_state` reflètent exactement le baseline. Ne pas fournir `ipv4_cidr`.

Après : attendre l'état terminal; vérifier `succeeded`, `result.verified: true` et `result.observed_changes.hostname` dans le read-back. Rejouer `show running-config | include ^hostname` et vérifier que l'interface est inchangée.

### B. Configuration d'une interface Ethernet

Avant : relever et archiver `show running-config interface <interface>` et `show ip interface brief`; confirmer que l'interface est dédiée, que l'adresse cible est libre dans le plan du lab et que la restauration est possible. La nouvelle IP/prefixe et l'état cible doivent être fournis et approuvés par l'opérateur; aucune adresse Cisco cible n'est définie dans les sources utilisées pour ce runbook.

Action : soumettre `cisco.ios.configure` avec `interfaces[0].name`, le `ipv4_cidr` approuvé, l'`admin_state` demandé et la description initiale exacte. Utiliser `max_attempts: 1`.

Après : vérifier l'exécution et `result.verified`; lire `show running-config interface <interface>` et `show ip interface brief`; comparer IP/masque, état administratif et description au résultat de read-back. Restaurer ensuite l'IP, le masque, l'état et la description initiaux par une nouvelle tâche approuvée, puis vérifier une nouvelle fois l'état réel. Aucune restauration automatique n'est promise en cas d'échec partiel.

### C. Échec volontaire sans mutation

Avant : relever l'état de l'interface comme pour B et vérifier qu'il correspond au baseline.

Action : envoyer une requête avec le même format, mais un CIDR volontairement invalide `192.0.2.1/33`. Cette valeur est une entrée négative pour le validateur uniquement; elle ne doit jamais atteindre IOS.

Après attendu : l'API répond `400 Bad Request`; aucun identifiant d'exécution n'est créé, aucun read-back d'exécuteur ne survient et l'état IOS reste inchangé. Vérifier ce dernier point par les commandes IOS et le résultat API réellement observés. Si une exécution est créée ou qu'une mutation se produit, arrêter le test et classer `FAIL`.

## Vérification des secrets dans les logs

Ne mettre aucun secret dans les tâches. Injecter les credentials depuis le gestionnaire de secrets vers l'environnement protégé du Provider; ne jamais activer le shell tracing (`set -x`). Après chaque cas, vérifier les journaux Provider/API réellement configurés avec l'outil d'inspection de secrets de l'organisation, sans imprimer les valeurs ni fournir de secrets sur la ligne de commande. Vérifier aussi que les JSON de requête/résultat et les réponses API n'incluent aucun champ d'authentification. Archiver seulement la commande d'inspection sans secret, son statut et un résultat agrégé; ne pas archiver un contenu de log contenant une valeur sensible.

## État observé pour cette passe

| Cas | Avant | Action | Après / read-back | Statut |
| --- | --- | --- | --- | --- |
| A — hostname | NON OBSERVÉ; aucune cible Cisco confirmée | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |
| B — interface IP/admin-state | NON OBSERVÉ; aucune cible Cisco confirmée | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |
| C — CIDR invalide | NON OBSERVÉ; API Provider de lab non démarrée pour ce test | NON ENVOYÉ | NON OBSERVÉ | NON EXÉCUTÉ |

Précondition pour poursuivre : l'opérateur PO/QA confirme par écrit le projet/nœud IOS GNS3, son adresse atteignable depuis le Provider, son modèle/version, l'interface de test isolée et l'autorisation de changement/restauration. Jusque-là, aucun succès de lab n'est déclaré.
