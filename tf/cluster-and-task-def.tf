terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 2.70"
    }
  }
}

provider "aws" {
  profile = "default"
  region  = var.region
}

resource "aws_iam_role" "jacktrip-task-execution-role" {
  name               = "jacktrip-task-execution-role"
  assume_role_policy = data.aws_iam_policy_document.ecs-task-assume-role.json
}

data "aws_iam_policy_document" "ecs-task-assume-role" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

# Normally we'd prefer not to hardcode an ARN in our Terraform, but since this is
# an AWS-managed policy, it's okay.
data "aws_iam_policy" "ecs-task-execution-role" {
  arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Attach the above policy to the execution role.
resource "aws_iam_role_policy_attachment" "ecs-task-execution-role" {
  role       = aws_iam_role.jacktrip-task-execution-role.name
  policy_arn = data.aws_iam_policy.ecs-task-execution-role.arn
}

resource "aws_cloudwatch_log_group" "jacktrip-cloudwatch-log-group" {
  name = "/ecs/run-jacktrip"
}

resource "aws_ecs_cluster" "jacktrip" {
  name = "jacktrip"
}

resource "aws_ecs_task_definition" "run-jacktrip" {
  family                = "run-jacktrip"
  requires_compatibilities = [ "FARGATE", "EC2" ]
  execution_role_arn = aws_iam_role.jacktrip-task-execution-role.arn
  cpu = "1024"
  memory = "2048"
  container_definitions =<<DEF
  [
    {
      "name": "jacktrip",
      "image": "docker.io/danhigham/jacktrip",
      "cpu": 1024,
      "memory": 2048,
      "essential": true,
      "environment": [{
        "name": "HUB_PATCH",
        "value": "2"
      }],
      "logConfiguration": {
        "logDriver": "awslogs",
        "options": {
          "awslogs-region": "${var.region}",
          "awslogs-group": "/ecs/run-jacktrip",
          "awslogs-stream-prefix": "ecs"
        }
      },
      "portMappings": [
        {
          "containerPort": 4464,
          "hostPort": 4464,
          "protocol": "tcp"
        }, {
          "containerPort": 4464,
          "hostPort": 4464,
          "protocol": "udp"
        }
      ]
    }
  ]
  DEF
  network_mode = "awsvpc"
}

resource "aws_vpc" "jacktrip" {
  cidr_block       = "10.0.0.0/16"
  instance_tenancy = "default"

  tags = {
    Name = "jacktrip"
  }
}

resource "aws_egress_only_internet_gateway" "egress" {
  vpc_id = aws_vpc.jacktrip.id

  tags = {
    Name = "jacktrip"
  }
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.jacktrip.id

  tags = {
    Name = "jacktrip-igw"
  }
}

resource "aws_subnet" "jacktrip" {
  vpc_id     = aws_vpc.jacktrip.id
  cidr_block = "10.0.1.0/24"
  map_public_ip_on_launch = true

  tags = {
    Name = "jacktrip"

  }
}

resource "aws_route_table" "jacktrip" {
  vpc_id = aws_vpc.jacktrip.id

  tags = {
    Name = "jacktrip"
  }
}

resource "aws_route_table_association" "a" {
  subnet_id      = aws_subnet.jacktrip.id
  route_table_id = aws_route_table.jacktrip.id
}

resource "aws_route" "r" {
  route_table_id  = aws_route_table.jacktrip.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id = aws_internet_gateway.igw.id
}

resource "aws_security_group" "jacktrip" {
  name        = "jacktrip"
  description = "Allow ingress for JackTrip"
  vpc_id      = aws_vpc.jacktrip.id

  ingress {
    description = "TCP Control"
    from_port   = 4464
    to_port     = 4464
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "UDP Control"
    from_port   = 4464
    to_port     = 4464
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "UDP Client Ports"
    from_port   = 61002
    to_port     = 62000
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

#   add ICMP

  tags = {
    Name = "jacktrip"
  }
}