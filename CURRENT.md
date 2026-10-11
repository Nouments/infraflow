# TÂCHE 1 — Corriger définitivement le démarrage InfraFlow dans GNS3

Dépôt : `Nouments/infraflow`
Branche de travail : `develop`

## Objectif

Corriger les erreurs réelles de démarrage des conteneurs `infraflow-agent` et `infraflow-provider`, puis rendre leur communication fonctionnelle dans GNS3.

Adresses du lab :
- Agent : `192.168.0.2/24`
- Provider : `192.168.0.3/24`
- gRPC : `8443`
- API : `8080`

## Règle fondamentale

**Ne crée aucun fichier d’exemple, aucune configuration de démonstration séparée et aucun fichier temporaire destiné uniquement à contourner les bugs.**

Je veux corriger l’implémentation existante et les fichiers réellement utilisés par les Dockerfiles, les entrypoints et les services.

Ne crée pas de nouveaux fichiers YAML de démonstration. Si une configuration de production doit être modifiée, corrige son fichier existant. Si une configuration GNS3 nécessite des paramètres spécifiques, utilise les mécanismes de configuration déjà prévus dans le projet ou ajoute uniquement le code de configuration indispensable à l’exécution réelle.

Ne committe aucun secret, token, clé privée ou certificat privé dans le dépôt public.

## 1. Trouver les causes racines

Inspecte :
- les structures de configuration Go de l’Agent et du Provider ;
- le décodage YAML et la validation stricte ;
- les commandes de démarrage ;
- les Dockerfiles et entrypoints ;
- les chemins des fichiers intégrés dans les images ;
- les variables d’environnement réellement prises en charge ;
- la configuration TLS et l’authentification ;
- les paramètres réseau et les répertoires de données.

Les erreurs observées indiquent que les champs YAML chargés ne correspondent pas aux structures Go attendues.

Détermine pourquoi les conteneurs chargent ces configurations et corrige la cause. Ne te contente pas de changer quelques noms de champs sans vérifier l’ensemble du contrat de configuration.

## 2. Corriger l’Agent

L’Agent doit :
- charger sa configuration avec le schéma réellement attendu par le code ;
- obtenir son identifiant, son site et ses capacités depuis la configuration prise en charge ;
- créer ou valider son répertoire de données ;
- lire le token depuis la variable d’environnement prévue ;
- joindre le Provider via `192.168.0.3:8443` dans le lab ;
- valider correctement le certificat TLS ;
- signaler clairement les erreurs de connexion, d’authentification et de configuration ;
- démarrer sans dépendre de fichiers absents ou de chemins incohérents.

## 3. Corriger le Provider

Le Provider doit :
- charger sa configuration réelle sans erreur YAML ;
- écouter sur les interfaces nécessaires à la communication entre conteneurs ;
- appliquer les règles TLS imposées par le code et la sécurité du projet ;
- créer ou valider ses répertoires de données ;
- charger le secret sans l’inscrire dans les fichiers versionnés ;
- exposer les services attendus sur les ports configurés ;
- fournir des erreurs explicites lorsqu’un certificat, une clé ou une variable indispensable manque.

## 4. Corriger le démarrage Docker et GNS3

Modifie les Dockerfiles et entrypoints existants si nécessaire.

Les images doivent embarquer les bons exécutables et les configurations réellement utilisées. Évite les décalages entre le code compilé, les chemins de configuration et les fichiers présents dans l’image.

Le fonctionnement ne doit pas dépendre de `localhost` pour joindre l’autre conteneur.

Si les adresses IP sont attribuées par GNS3, ne suppose pas que Docker configure automatiquement une adresse statique. Utilise les mécanismes compatibles avec le réseau GNS3 et documente uniquement les paramètres indispensables au déploiement.

Si des certificats sont nécessaires, prévois leur génération ou leur injection hors du dépôt public. N’intègre aucune clé privée dans Git ou dans une image publique.

## 5. Tests obligatoires

Ajoute ou adapte les tests dans les emplacements de tests existants. Ne crée pas de fichiers d’exemple pour les faire passer.

Vérifie :
- les configurations valides et invalides ;
- le rejet des champs inconnus ;
- les variables d’environnement obligatoires ;
- la validation TLS et les erreurs de certificats ;
- les chemins de données ;
- le démarrage des services ;
- la communication authentifiée entre Agent et Provider.

Exécute, lorsque l’environnement le permet :
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- la compilation des deux binaires ;
- la construction des deux images Docker ;
- un test d’intégration réel entre les deux services.

Ne présente pas un test unitaire comme une preuve de fonctionnement dans GNS3. Si le lab n’est pas accessible, indique précisément les validations qui restent à effectuer.

## 6. Protection du dépôt public

Avant et après les modifications :
- vérifie `git status` ;
- préserve les changements préexistants ;
- ne supprime pas de travaux sans rapport avec cette tâche ;
- n’ajoute aucun token, mot de passe, certificat privé ou clé privée ;
- vérifie les nouveaux fichiers avec `git diff --check` et `git diff` ;
- ne pousse pas de modifications sans mon autorisation.

## 7. Résultat attendu

Termine la correction avant de répondre.

Fournis :
1. les causes racines confirmées ;
2. les fichiers existants modifiés et la raison de chaque modification ;
3. les corrections appliquées ;
4. les commandes de validation exécutées avec leurs résultats réels ;
5. les instructions minimales pour reconstruire les images et tester les conteneurs dans GNS3 ;
6. les problèmes restants et les validations non effectuées.

Ne passe pas à la tâche suivante. Ne prétends pas que le problème est résolu sans preuve.

**Priorité : corriger le code existant, garder le dépôt public propre et obtenir un démarrage réellement fonctionnel.**