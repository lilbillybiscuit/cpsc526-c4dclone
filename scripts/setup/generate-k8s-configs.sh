#!/bin/bash

# Create central-server configurations
mkdir -p deployments/central-server/{task-server,c4d-server,failure-server,mvcc}

# Create task-server configs
cat > deployments/central-server/task-server/deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: task-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: task-server
  template:
    metadata:
      labels:
        app: task-server
    spec:
      containers:
      - name: task-server
        image: task-server:latest
        ports:
        - containerPort: 8080
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: task-server
spec:
  selector:
    app: task-server
  ports:
  - port: 8080
    targetPort: 8080
  type: ClusterIP
EOF

# Create c4d-server configs
cat > deployments/central-server/c4d-server/deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: c4d-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: c4d-server
  template:
    metadata:
      labels:
        app: c4d-server
    spec:
      containers:
      - name: c4d-server
        image: c4d-server:latest
        ports:
        - containerPort: 8081
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: c4d-server
spec:
  selector:
    app: c4d-server
  ports:
  - port: 8081
    targetPort: 8081
  type: ClusterIP
EOF

# Create failure-server configs
cat > deployments/central-server/failure-server/deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: failure-server
spec:
  replicas: 1
  selector:
    matchLabels:
      app: failure-server
  template:
    metadata:
      labels:
        app: failure-server
    spec:
      containers:
      - name: failure-server
        image: failure-server:latest
        ports:
        - containerPort: 8082
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: failure-server
spec:
  selector:
    app: failure-server
  ports:
  - port: 8082
    targetPort: 8082
  type: ClusterIP
EOF

# Create central MVCC configs
cat > deployments/central-server/mvcc/statefulset.yaml << 'EOF'
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: central-mvcc
spec:
  serviceName: central-mvcc
  replicas: 3
  selector:
    matchLabels:
      app: central-mvcc
  template:
    metadata:
      labels:
        app: central-mvcc
    spec:
      containers:
      - name: mvcc
        image: postgres:13
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: mvcc-secrets
              key: postgres-password
        volumeMounts:
        - name: mvcc-storage
          mountPath: /var/lib/postgresql/data
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
  volumeClaimTemplates:
  - metadata:
      name: mvcc-storage
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: central-mvcc
spec:
  selector:
    app: central-mvcc
  ports:
  - port: 5432
    targetPort: 5432
  clusterIP: None
EOF

# Create local group configurations
mkdir -p deployments/local-group/{compute-node,mvcc,network-policies}

# Create compute node configs
cat > deployments/local-group/compute-node/deployment.yaml << 'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: compute-node
spec:
  replicas: 1
  selector:
    matchLabels:
      app: compute-node
  template:
    metadata:
      labels:
        app: compute-node
    spec:
      containers:
      - name: compute-engine
        image: compute-engine:latest
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"
        volumeMounts:
        - name: shared-data
          mountPath: /data
        
      - name: c4d-agent
        image: c4d-agent:latest
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        
      - name: failure-agent
        image: failure-agent:latest
        securityContext:
          privileged: true  # Required for network manipulation
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        
      - name: storage-engine
        image: storage-engine:latest
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        volumeMounts:
        - name: shared-data
          mountPath: /data
      
      volumes:
      - name: shared-data
        emptyDir: {}
EOF

# Create local MVCC configs
cat > deployments/local-group/mvcc/statefulset.yaml << 'EOF'
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: local-mvcc
spec:
  serviceName: local-mvcc
  replicas: 2
  selector:
    matchLabels:
      app: local-mvcc
  template:
    metadata:
      labels:
        app: local-mvcc
    spec:
      containers:
      - name: mvcc
        image: postgres:13
        ports:
        - containerPort: 5432
        env:
        - name: POSTGRES_PASSWORD
          valueFrom:
            secretKeyRef:
              name: local-mvcc-secrets
              key: postgres-password
        volumeMounts:
        - name: mvcc-storage
          mountPath: /var/lib/postgresql/data
        resources:
          requests:
            memory: "1Gi"
            cpu: "500m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
  volumeClaimTemplates:
  - metadata:
      name: mvcc-storage
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: local-mvcc
spec:
  selector:
    app: local-mvcc
  ports:
  - port: 5432
    targetPort: 5432
  clusterIP: None
EOF

# Create network policies
cat > deployments/local-group/network-policies/network-policy.yaml << 'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: local-group-network
spec:
  podSelector:
    matchLabels:
      local-group: "true"
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          local-group: "true"
    ports:
    - protocol: TCP
  egress:
  - to:
    - podSelector:
        matchLabels:
          local-group: "true"
    ports:
    - protocol: TCP
  - to:
    - namespaceSelector:
        matchLabels:
          name: central-services
    ports:
    - protocol: TCP
EOF

# Create secrets
cat > deployments/central-server/mvcc/secrets.yaml << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: mvcc-secrets
type: Opaque
data:
  postgres-password: cGFzc3dvcmQ=  # 'password' in base64
EOF

cat > deployments/local-group/mvcc/secrets.yaml << 'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: local-mvcc-secrets
type: Opaque
data:
  postgres-password: cGFzc3dvcmQ=  # 'password' in base64
EOF

# Make the script executable
chmod +x scripts/setup/generate-k8s-configs.sh

echo "Kubernetes configurations generated successfully!"
