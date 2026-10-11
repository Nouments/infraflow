# INFRAFLOW — PHASE 2 : CYCLE DE VIE RÉEL DES TÂCHES

Repository : `Nouments/infraflow`  
Branche : `develop`

## Objectif

Relier le moteur de tâches existant au Provider pour construire un véritable cycle de vie d’exécution, persistant, observable, sécurisé et prêt à recevoir les adaptateurs réseau dans la phase suivante.

Ne reconstruis pas les images Docker et ne bloque pas le développement sur GNS3. Implémente et vérifie tout ce qui peut l’être localement.

## 1. Audit obligatoire avant modification

Lis le code réel avant de changer quoi que ce soit, en particulier :

- `internal/application/` : scheduler, jobs, ports et cas d’usage ;
- `internal/domain/` et `internal/ports/` ;
- `provider/` : API HTTP, services, persistance, authentification et assemblage de l’application ;
- `api/proto/` et `pkg/protocol/` ;
- les tests existants ;
- `INFRAFLOW_SPEC.md`, `ARCHITECTURE.md`, `README.md` et `CURRENT.md`.

Identifie les implémentations existantes et réutilise-les. Ne crée pas un second scheduler ou un deuxième système de jobs qui ferait doublon.

## 2. Séparer clairement planification et exécution

Conserve le comportement actuel de validation, de planification et de génération.

Une tâche planifiée ne doit jamais être considérée comme exécutée simplement parce que le plan ou les artefacts ont été générés.

Introduis un contrat d’application explicite pour soumettre et suivre les tâches d’exécution. Sépare les états de planification des états d’exécution si leur fusion rend les transitions ambiguës.

Sans exécuteur opérationnel enregistré, le Provider doit refuser ou bloquer explicitement une exécution non prise en charge. Il ne doit jamais annoncer un succès fictif.

## 3. Cycle de vie persistant

Implémente les états nécessaires, avec des transitions validées, notamment :

- `queued` ;
- `running` ;
- `succeeded` ;
- `failed` ;
- `cancelled` ;
- `timed_out`.

Ajoute les états intermédiaires nécessaires uniquement si l’architecture le justifie.

Chaque tâche doit conserver au minimum son identifiant, son type, son état, ses dates de création et de mise à jour, ses paramètres validés, ses tentatives, son résultat, son erreur et les informations utiles à l’audit.

La persistance doit survivre au redémarrage du Provider. Une tâche interrompue par un arrêt du service ne doit pas rester indéfiniment en `running` sans politique explicite de récupération.

## 4. Connecter réellement le scheduler

Relie le scheduler existant au nouveau cas d’usage d’exécution, à travers des ports définis par la couche application.

Le moteur doit gérer correctement :

- les dépendances et l’ordre d’exécution ;
- les limites de concurrence ;
- les verrous de ressources ;
- les délais d’exécution ;
- les nouvelles tentatives selon une politique explicite ;
- l’annulation avant et pendant l’exécution ;
- les erreurs et la propagation des résultats ;
- l’arrêt propre du service.

Empêche les doubles soumissions et les transitions concurrentes incohérentes. Prévois une clé d’idempotence ou un mécanisme équivalent si une même requête peut être rejouée.

Une annulation demandée ne signifie pas que la tâche est déjà annulée : l’état final doit refléter le résultat réel de l’arrêt.

## 5. Contrat d’exécution sécurisé

Définis une interface générique d’exécuteur permettant d’ajouter ultérieurement les adaptateurs réseau sans réécrire le scheduler.

L’exécuteur doit recevoir une tâche validée et produire un résultat structuré avec statut, sortie, erreur et durée lorsque disponible.

N’exécute pas de commande shell arbitraire fournie par l’API. Ne lance pas de commande réseau, Ansible, Terraform ou SSH sans un exécuteur explicitement enregistré et une politique autorisant cette opération.

Aucun adaptateur Cisco, MikroTik, FortiGate ou Proxmox n’est demandé dans cette phase.

## 6. API et autorisations

Expose les opérations nécessaires pour :

- soumettre une tâche d’exécution ;
- consulter une tâche ;
- lister les tâches avec filtres et pagination ;
- demander son annulation ;
- consulter ses tentatives, résultats, erreurs et événements.

Respecte le système actuel de sessions, rôles et RBAC. Vérifie les autorisations pour chaque opération, notamment l’annulation et l’accès aux résultats.

Ne casse pas les routes existantes. Versionne les changements de contrat si nécessaire et documente les éventuelles modifications protobuf.

## 7. Rapports, journaux et audit

Chaque transition significative doit être observable et persistée. Enregistre l’acteur, la tâche, l’action, l’horodatage et le résultat dans le mécanisme d’audit existant.

Les erreurs doivent être exploitables pour diagnostiquer une tâche. Ne stocke jamais de mots de passe, jetons, clés privées ou autres secrets dans les paramètres, les sorties, les événements ou les journaux.

Réutilise les mécanismes de journalisation existants. Ne crée pas une deuxième chaîne de logs.

## 8. Tests exigés dans cette phase

Ajoute des tests ciblés portant sur le comportement réel :

- transitions autorisées et refusées ;
- persistance et rechargement après redémarrage ;
- succès, échec et propagation des erreurs ;
- dépendances, concurrence et verrous ;
- retries et délais ;
- annulation avant et pendant une tâche ;
- idempotence et soumissions concurrentes ;
- authentification et RBAC de chaque route ;
- absence de faux succès quand aucun exécuteur n’est disponible ;
- audit et absence de secrets dans les journaux.

Utilise des exécuteurs de test déterministes pour tester le contrat et le scheduler, sans prétendre tester des équipements réseau. Ne remplace pas les tests comportementaux par de simples tests de mocks.

Lance les tests locaux ciblés et les vérifications légères possibles. Ne lance aucune reconstruction Docker lourde et ne prétends pas avoir validé GNS3 ou du matériel réel.

## 9. Documentation et suivi

Mets à jour `INFRAFLOW_SPEC.md`, `ARCHITECTURE.md`, `README.md` et `CURRENT.md` en fonction du code réellement livré.

Marque une fonctionnalité comme implémentée uniquement si son code et ses tests le justifient. Distingue les tests exécutés des tests restant à faire.

Ne crée pas de nouveaux fichiers d’exemples YAML ou de données de démonstration. Ne commite aucun secret.

## 10. Compte rendu obligatoire

À la fin, fournis :

1. Les défauts constatés avant les modifications.
2. Les fichiers modifiés et la raison de chaque modification.
3. Le fonctionnement précis du nouveau cycle de vie.
4. Les routes et contrats ajoutés ou modifiés.
5. Les tests exécutés, avec les commandes et résultats réels.
6. Les limitations restantes et les tests reportés à la campagne finale.
7. La prochaine tâche recommandée.

Ne commence pas les adaptateurs fournisseurs dans cette phase. Ne pousse pas de modifications sans autorisation. N’affirme pas que toute la phase est terminée si une exigence reste incomplète.

**Critère de fin :** le Provider possède un cycle d’exécution générique persistant, sécurisé et relié au scheduler, sans confondre planification, génération et exécution réelle.